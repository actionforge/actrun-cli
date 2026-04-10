#!/usr/bin/env python
# -*- coding: utf-8 -*-

"""
Runs all .sh files in "tests_e2e/scripts".
These scripts contain "#! test <command>" comments.
They are expanded, executed and then diffed against the output in
"tests/integrations/references".

Platform-specific tests:
    Scripts suffixed with _linux, _darwin, or _windows (e.g., docker-alpine_linux.sh)
    only run on that platform. Scripts without a suffix run on all platforms.

Build tags (p4):
    Some nodes require Go build tags to be compiled in. P4 nodes (core/p4-run@v1,
    core/p4-sync@v1, etc.) are guarded by `//go:build p4` and require the P4 API SDK.
    The test runner auto-detects the P4 API SDK in `p4api/` and builds with `-tags=p4`
    when available. Run `bash setup.sh` to download the SDK if not present.

    To add a new P4 e2e test:
    1. Run `bash setup.sh` to download the P4 API SDK (if not already done)
    2. Create a .sh script in tests_e2e/scripts/ (no platform suffix = runs everywhere)
    3. Set P4 env vars (P4PORT, P4USER, P4PASSWD) and use `#! test actrun <graph_file>`
    4. Run `python tests_e2e/tests_e2e.py p4_connect.sh` to generate the reference file
    5. Commit the reference file — it must match output for the tagged build in CI
    6. See tests_e2e/scripts/p4_connect.sh for an example
"""

import os
import subprocess
import sys
import shutil
import shlex
import re
import platform
import tempfile
import concurrent.futures
import io
from pathlib import Path

# Ensure Unicode output works on Windows cp1252 consoles (affects this process and all children).
os.environ["PYTHONIOENCODING"] = "utf-8"
if sys.stdout.encoding and sys.stdout.encoding.lower() != "utf-8":
    sys.stdout = io.TextIOWrapper(sys.stdout.buffer, encoding="utf-8", errors="replace")
if sys.stderr.encoding and sys.stderr.encoding.lower() != "utf-8":
    sys.stderr = io.TextIOWrapper(sys.stderr.buffer, encoding="utf-8", errors="replace")

# Setup paths
CURRENT_DIR = Path(__file__).parent.absolute()
DEPS_PATH = CURRENT_DIR / "deps"
sys.path.append(str(DEPS_PATH))

COVERAGE = False

# pylint: disable-next=import-error,wrong-import-position
from dotenv import dotenv_values

GLOBAL_ENVS = os.environ.copy()


def print_env_vars_redacted(env_vars: dict):
    sensitive_keywords = ("key", "access", "secret", "token")
    for k, v in env_vars.items():
        if any(word in k.lower() for word in sensitive_keywords):
            print(f"    {k}=<REDACTED>")
        else:
            print(f"    {k}={v}")

print("Using the following env vars for the tests:")
print_env_vars_redacted(GLOBAL_ENVS)

# Remove all ACT_ env vars except those starting with ACT_INPUT_SECRET_
GLOBAL_ENVS = {k: v for k, v in GLOBAL_ENVS.items() if not (k.startswith("ACT_") and not k.startswith("ACT_INPUT_SECRET"))}

env_vars = dotenv_values(".env")
if len(env_vars) > 0:
    if not any(k.startswith("INPUT_") for k in env_vars):
        print("‼️ no env vars prefixed with 'INPUT_' found in .env")
        sys.exit(1)
    env_vars = {f"ACT_{k}": v for k, v in env_vars.items() if k.startswith("INPUT_")}
    GLOBAL_ENVS.update(env_vars)

print("Using the following env vars after loading .env:")
print_env_vars_redacted(GLOBAL_ENVS)

GLOBAL_ENVS = {k: v for k, v in GLOBAL_ENVS.items() if not k.startswith(("GITHUB_", "ACTIONS_", "RUNNER_"))}

print("Using the following env vars after cleanup:")
print_env_vars_redacted(GLOBAL_ENVS)

IS_WINDOWS = sys.platform == "win32"

PLATFORM_MAP = {
    "linux": "linux",
    "darwin": "darwin",
    "win32": "windows",
}
CURRENT_PLATFORM = PLATFORM_MAP.get(sys.platform, sys.platform)
ALL_PLATFORMS = ["linux", "darwin", "windows"]

def get_script_platform(script_path: str) -> str | None:
    name = Path(script_path).stem  # removes .sh
    for plat in ALL_PLATFORMS:
        if name.endswith(f"_{plat}"):
            return plat
    return None

def should_run_on_current_platform(script_path: str) -> bool:
    script_platform = get_script_platform(script_path)
    if script_platform is None:
        return True  # No suffix means run everywhere
    return script_platform == CURRENT_PLATFORM

# --- Helper Classes ---

class Style:
    """Just some ANSI codes."""
    MAGENTA = '\033[95m'
    BLUE = '\033[94m'
    CYAN = '\033[96m'
    GRAY = '\033[90m'
    GREEN = '\033[92m'
    YELLOW = '\033[93m'
    RED = '\033[91m'
    RESET = '\033[0m'
    BOLD = '\033[1m'
    UNDERLINE = '\033[4m'


# --- Path & String Helpers ---

def get_redact_function_script() -> str:
    """Generates the bash function string to redact absolute paths."""
    # TODO: (Seb) this is kinda ugly, maybe move to a template file later?
    redact_script_path = str(CURRENT_DIR / 'redact.py')
    python_exe = sys.executable

    if IS_WINDOWS:
        # Escape backslashes for Windows shell usage
        redact_script_path = redact_script_path.replace("\\", "\\\\")
        executable_name = os.path.basename(python_exe)
        
        return f"""redact_abs_paths() {{
        {executable_name} {redact_script_path}
}}
"""
    else:
        return f"""redact_abs_paths() {{
        {python_exe} {redact_script_path}
    }}
    """

def to_posix_path(path_str: str) -> str:
    """
    Forces POSIX paths (forward slashes).
    Essential for MinGW/Git Bash on Windows.
    """
    if not IS_WINDOWS:
        return path_str
        
    p = Path(path_str).as_posix()
    # Remove drive colon and ensure a leading slash (eg C:/Users -> /c/Users)
    cleaned = p.replace(':', '')
    return "/" + cleaned.lstrip("/")

def collect_shell_scripts(directory: str) -> list[str]:
    all_scripts = [str(p) for p in Path(directory).rglob("*.sh")]
    filtered = []
    for script in all_scripts:
        if should_run_on_current_platform(script):
            filtered.append(script)
        else:
            script_platform = get_script_platform(script)
            print(f"Skipping {os.path.basename(script)} (platform: {script_platform}, current: {CURRENT_PLATFORM})")
    return filtered

STACK_TRACE_LINE_PATTERN = re.compile(r'(\t\S+:)\d+')

def normalize_stack_trace_lines(ref_dir: str, script_name: str):
    """Normalize unstable line numbers in stack traces to -1."""
    for ref_file in Path(ref_dir).glob(f"reference_{script_name}_l*"):
        content = ref_file.read_text(encoding="utf-8")
        modified = STACK_TRACE_LINE_PATTERN.sub(r'\g<1>-1', content)
        if modified != content:
            ref_file.write_text(modified, encoding="utf-8")

def create_temp_script() -> str:
    fd, path = tempfile.mkstemp(suffix=".sh")
    os.close(fd)
    return path

def verify_system_requirements():
    # look for bash and pwsh in PATH or common locations
    bash_candidates = ["bash"]
    if IS_WINDOWS:
        bash_candidates.extend([
            r"C:\Program Files\Git\bin\bash.exe",
            r"C:\msys64\usr\bin\bash.exe",
            r"C:\cygwin64\bin\bash.exe"
        ])

    bash_found = False
    for candidate in bash_candidates:
        # specific check for absolute paths on windows, or generic command check
        if shutil.which(candidate) or (os.path.isabs(candidate) and os.path.exists(candidate)):
            try:
                subprocess.run([candidate, "--version"], stdout=subprocess.DEVNULL, stderr=subprocess.STDOUT, check=False)
                bash_found = True
                break
            except OSError:
                continue
    
    if not bash_found:
        print(f"{Style.RED}‼️ bash is not installed.{Style.RESET}")
        sys.exit(1)

    if not shutil.which("pwsh"):
        print(f"{Style.RED}‼️ pwsh is not installed.{Style.RESET}")
        sys.exit(1)

def run_test_script(root_path: str, script_file: str, working_dir: str):
    """
    Executes the generated bash script.
    """
    env = GLOBAL_ENVS.copy()
    env["LC_ALL"] = "C" # set to keep sorting consistent when using sort pipe
    
    py_exe = sys.executable.replace('\\', '/') if os.name == 'nt' else sys.executable

    #aAdd distinct path for python deps
    python_path = env.get("PYTHONPATH", "")
    env["PYTHONPATH"] = f"{str(DEPS_PATH)}{os.pathsep}{python_path}" if python_path else str(DEPS_PATH)

    # Construct PATH: add ./dist to front
    dist_path = os.path.join(root_path, "dist")
    env["PATH"] = f"{dist_path}{os.pathsep}{env['PATH']}"

    env.update({
        "ACT_NOCOLOR": "true",
        "ACT_TESTE2E": "true",
        "ACT_LOGLEVEL": "debug",
        "ACT_ROOT": root_path.replace('\\', '/'),
        "ACT_GRAPH_FILES_DIR": str(Path(__file__).parent / "scripts"),
        "PYTHON_EXECUTABLE": py_exe,
        "PATH_SEPARATOR": os.sep
    })

    return subprocess.run(
        ["bash", to_posix_path(script_file)],
        shell=IS_WINDOWS,
        env=env,
        cwd=working_dir,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
        encoding='utf-8',
        check=False
    )

def process_and_run_test(root_dir: str, source_script: str, ref_dir: str, cov_dir: str):
    output = io.StringIO()
    temp_script_path = create_temp_script()
    redact_func = get_redact_function_script()
    script_name = os.path.basename(source_script)

    with open(source_script, encoding="utf-8") as src, open(temp_script_path, "w", encoding="utf-8") as dest:
        dest.write(redact_func + "\n")

        current_func_name = None

        for lineno, line in enumerate(src, 1):
            if lineno == 1 and line.startswith("#!"):
                continue

            # find test commands: "#! test <cmd>"
            match = re.match(r"#!\s(.*)", line)
            if match and match.group(1):
                # clean up the command string.
                test_cmd = re.sub(r"#!\stest(.*)", r"\1", line).strip()

                ref_file = to_posix_path(f"{ref_dir}/reference_{script_name}_l{lineno}")
                cov_file = to_posix_path(f"{cov_dir}/coverage_{script_name}_l{lineno}")

                if COVERAGE and test_cmd.startswith("actrun"):
                    test_cmd = f'actrun -test.coverprofile="{cov_file}" ' + test_cmd[6:]

                # write the echo command for logs
                dest.write(f"echo % {Style.GRAY}L{lineno} $ {shlex.quote(test_cmd)}{Style.RESET}\n")
                
                # write command output to temp file first, then redact and save as reference.
                # Piping directly from the command can lose output if it crashes (exit 127).
                tmp_ref = ref_file + ".tmp"
                dest.write(f"{test_cmd} > {tmp_ref} 2>&1 || true\n")
                dest.write(f"redact_abs_paths < {tmp_ref} | tr -d '\\r' > {ref_file}\n")
                dest.write(f"rm -f {tmp_ref}\n")
                dest.write(f"cat {ref_file}\n")
            
            else:
                # here are all the other non test lines
                stripped = line.strip()
                if stripped:
                    if stripped.startswith("function"):
                        fname = stripped.split()[1] if len(stripped.split()) > 1 else "unknown"
                        raise RuntimeError(f"'function' keyword is not POSIX compliant. Use '{fname}() {{' instead.")
                    
                    if not current_func_name and stripped.endswith("() {"):
                        current_func_name = stripped.split()[0]
                    elif stripped == "}":
                        if not current_func_name:
                            raise RuntimeError(f"Closing brace without function definition in {source_script}:{lineno}")
                        current_func_name = None
                    elif not stripped.startswith("#"):
                        # echo line if not inside a function definition
                        if not current_func_name:
                            dest.write(f"echo {Style.BLUE}L{lineno} $ {shlex.quote(stripped)}{Style.RESET}\n")
                
                dest.write(line)

        if current_func_name:
            raise RuntimeError(f"Function {current_func_name} was never closed.")

    tmp_cwd = tempfile.mkdtemp(prefix=f"actrun.{script_name}")
    output.write(f"Running script: {source_script} -> {temp_script_path}:\n           cwd: {tmp_cwd}\n\n")
    result = run_test_script(root_dir, temp_script_path, tmp_cwd)
    if result.stdout:
        output.write(result.stdout)
    normalize_stack_trace_lines(ref_dir, script_name)
    return output.getvalue(), result.returncode == 0

def get_p4_build_config() -> dict | None:
    """Detect P4 API SDK and return CGO flags for building with p4 tag.
    Returns None if P4 SDK is not available."""
    p4api_dir = Path(os.getcwd()) / "p4api"
    if not p4api_dir.exists():
        return None

    p4_include = str(p4api_dir / "include")
    arch = platform.machine().lower()
    if arch in ("aarch64", "arm64"):
        arch = "arm64"
    else:
        arch = "x64"

    if sys.platform == "linux":
        lib_name = "linux-aarch64" if arch == "arm64" else "linux-x86_64"
        p4_lib = str(p4api_dir / lib_name / "lib")
        ssl_lib = str(p4api_dir / f"ssl-linux-{arch}" / "lib")
        if not Path(p4_lib).exists():
            return None
        cgo_cppflags = f"-I{p4_include}"
        if Path(ssl_lib).exists():
            cgo_ldflags = f"-L{p4_lib} -lp4api {ssl_lib}/libssl.a {ssl_lib}/libcrypto.a"
        else:
            cgo_ldflags = f"-L{p4_lib} -lp4api -lssl -lcrypto"
    elif sys.platform == "darwin":
        p4_lib = str(p4api_dir / "macos" / "lib")
        ssl_lib = str(p4api_dir / f"ssl-macos-{arch}" / "lib")
        if not Path(p4_lib).exists():
            return None
        cgo_cppflags = f"-I{p4_include}"
        cgo_ldflags = f"-L{p4_lib} -lp4api {ssl_lib}/libssl.a {ssl_lib}/libcrypto.a -framework ApplicationServices -framework Foundation -framework Security -framework CoreFoundation"
    elif sys.platform == "win32":
        if arch == "arm64":
            return None  # P4 not supported on Windows ARM64
        p4_lib = (p4api_dir / "windows-x86_64" / "lib").as_posix()
        if not Path(p4_lib).exists():
            return None
        # Use forward-slash Windows paths (C:/...) for CGo compatibility
        p4_include = Path(p4_include).as_posix()
        cgo_cppflags = f"-I{p4_include} -DOS_NT"
        ssl_lib = (p4api_dir / "ssl-windows-x64" / "lib").as_posix()
        if not (Path(ssl_lib).exists() and (Path(ssl_lib) / "libssl.a").exists()):
            return None
        cgo_ldflags = f"-L{p4_lib} -L{ssl_lib} -lp4api -lssl -lcrypto -lcrypt32 -lws2_32 -lole32 -lshell32 -luser32 -ladvapi32"
    else:
        return None

    return {
        "CGO_ENABLED": "1",
        "CGO_CPPFLAGS": cgo_cppflags,
        "CGO_LDFLAGS": cgo_ldflags,
    }


def compile_binaries(is_github_runner: bool):
    if is_github_runner:
        return

    # build CLI
    cli_out = 'dist/actrun' + ('.exe' if IS_WINDOWS else '')

    env = GLOBAL_ENVS.copy()
    env["GCFLAGS"] = "-N -l"

    # Auto-download P4 API SDK if not present
    p4api_dir = Path(os.getcwd()) / "p4api"
    setup_sh = Path(os.getcwd()) / "setup.sh"
    if setup_sh.exists() and not get_p4_build_config():
        print("P4 API SDK not found, running setup.sh to download...")
        result = subprocess.run(["bash", str(setup_sh)], capture_output=True, text=True)
        if result.returncode == 0:
            print(result.stdout.strip())
        else:
            print(f"setup.sh failed: {result.stderr.strip()}")

    # Auto-build static OpenSSL if not present (one-time cost)
    setup_openssl_sh = Path(os.getcwd()) / "setup-openssl.sh"
    if setup_openssl_sh.exists() and p4api_dir.exists():
        arch = "arm64" if platform.machine().lower() in ("aarch64", "arm64") else "x64"
        ssl_os_map = {"linux": "linux", "darwin": "macos", "win32": "windows"}
        ssl_os = ssl_os_map.get(sys.platform)
        if ssl_os:
            ssl_arch_suffix = {"linux": f"ssl-linux-{arch}", "macos": f"ssl-macos-{arch}", "windows": "ssl-windows-x64"}
            ssl_dir = p4api_dir / ssl_arch_suffix[ssl_os] / "lib"
            if not (ssl_dir / "libssl.a").exists():
                print(f"Static OpenSSL not found, building via setup-openssl.sh (one-time)...")
                if IS_WINDOWS:
                    # Must run inside MSYS2 shell — Chocolatey's as.exe has temp dir issues
                    msys2_shell = Path(r"C:\msys64\msys2_shell.cmd")
                    if not msys2_shell.exists():
                        print("MSYS2 not found at C:\\msys64. Install MSYS2 to build static OpenSSL.")
                        sys.exit(1)
                    ssl_dir_posix = ssl_dir.as_posix()
                    result = subprocess.run(
                        [str(msys2_shell), "-defterm", "-here", "-no-start", "-mingw64", "-c",
                         f"cd '{Path(os.getcwd()).as_posix()}' && bash setup-openssl.sh {ssl_os} {arch} '{ssl_dir_posix}'"],
                        capture_output=True, text=True)
                else:
                    result = subprocess.run(
                        ["bash", str(setup_openssl_sh), ssl_os, arch, str(ssl_dir)],
                        capture_output=True, text=True)
                if result.returncode == 0:
                    print(result.stdout.strip())
                else:
                    print(f"setup-openssl.sh failed: {result.stderr.strip()}")
                    sys.exit(1)

    # On Windows, use MSYS2 GCC for ABI compatibility with P4 SDK and OpenSSL
    if IS_WINDOWS:
        msys2_gcc = Path(r"C:\msys64\mingw64\bin")
        if msys2_gcc.exists():
            env["PATH"] = str(msys2_gcc) + os.pathsep + env.get("PATH", "")

    # Detect P4 API SDK — P4 support is always required
    tags = []
    p4_config = get_p4_build_config()
    if p4_config:
        tags.append("p4")
        env.update(p4_config)
        print(f"P4 API SDK detected, building with -tags=p4")
    else:
        print(f"P4 API SDK not found. Run 'bash setup.sh' to download it.")
        sys.exit(1)

    build_cmd = ['go', 'build']
    if tags:
        build_cmd.append(f'-tags={",".join(tags)}')
    build_cmd.extend(['-o', cli_out, '.'])

    if COVERAGE:
        # TODO: (Seb) coverage build takes ages
        coverage_tags = ['main_test'] + tags
        build_cmd = ['go', 'test', '.', '-buildvcs=true', '-cover', '-coverprofile', f'-tags={",".join(coverage_tags)}', '-c', '-o', cli_out]

    print(f"Building {cli_out}")
    subprocess.run(build_cmd, stdout=sys.stdout, stderr=subprocess.STDOUT, check=True, env=env)

    # build the python shared lib
    lib_ext = {'linux': 'so', 'darwin': 'so', 'win32': 'dll'}.get(sys.platform)
    lib_out = f'dist/actrun.{lib_ext}'
    
    py_args = ['go', 'build', '-tags=api,cpython', '-buildmode=c-shared', '-o', lib_out, '.']
    py_env = GLOBAL_ENVS.copy()
    py_env["CGO_ENABLED"] = "1"
    
    print(f"Building {lib_out}")
    subprocess.run(py_args, stdout=sys.stdout, stderr=subprocess.STDOUT, check=True, env=py_env)

def get_shared_lib_path(is_github_runner: bool) -> str:
    lib_ext = {'linux': 'so', 'darwin': 'so', 'win32': 'dll'}.get(sys.platform)
    
    if not is_github_runner:
        return os.path.join(os.getcwd(), 'dist', f'actrun.{lib_ext}')
    
    os_map = {"darwin": "macos", "windows": "windows", "linux": "linux"}
    arch_map = {"x86_64": "x64", "amd64": "x64", "arm64": "arm64", "aarch64": "arm64"}
    
    current_os = os_map[platform.system().lower()]
    current_arch = arch_map[platform.machine().lower()]
    
    return os.path.join(os.getcwd(), 'dist', f'actrun-py-{current_os}-{current_arch}.{lib_ext}')


def main():
    verify_system_requirements()
    
    is_gh_actions = os.getenv("GITHUB_ACTIONS", "false").lower() == "true"
    print(f"Running end-to-end tests (is_github_actions={is_gh_actions})")

    # cli arg parsing
    target_test = sys.argv[1] if len(sys.argv) > 1 else None

    # dir setup
    base_cwd = os.getcwd()
    ref_dir = os.path.join(base_cwd, "tests_e2e", "references")
    scripts_dir = os.path.join(base_cwd, "tests_e2e", "scripts")
    cov_dir = os.path.join(base_cwd, "tests_e2e", "coverage")
    
    os.makedirs(cov_dir, exist_ok=True)

    # delete refs if running full suite, but preserve refs from other platforms
    if target_test is None:
        if os.path.exists(ref_dir):
            for ref_file in os.listdir(ref_dir):
                # Check if this reference file belongs to another platform
                # Reference files are named: reference_{script_name}_l{lineno}
                # For platform-specific: reference_docker-alpine_linux.sh_l11
                is_other_platform = False
                for plat in ALL_PLATFORMS:
                    if plat != CURRENT_PLATFORM and f"_{plat}.sh_" in ref_file:
                        is_other_platform = True
                        break

                if not is_other_platform:
                    os.remove(os.path.join(ref_dir, ref_file))
        else:
            os.makedirs(ref_dir, exist_ok=True)

    compile_binaries(is_gh_actions)
    
    GLOBAL_ENVS['ACT_SHARED_LIB_PATH'] = get_shared_lib_path(is_gh_actions)

    # Run Tests
    if target_test is None:
        scripts = collect_shell_scripts(scripts_dir)
    else:
        scripts = [os.path.join(scripts_dir, target_test)]

    failed = []
    with concurrent.futures.ThreadPoolExecutor(max_workers=os.cpu_count()) as executor:
        future_to_script = {
            executor.submit(process_and_run_test, base_cwd, script, ref_dir, cov_dir): script
            for script in scripts
        }
        for future in concurrent.futures.as_completed(future_to_script):
            script = future_to_script[future]
            script_name = os.path.basename(script)
            try:
                test_output, success = future.result()
                print(f"\n{'='*60}")
                print(f" {script_name}")
                print(f"{'='*60}")
                print(test_output, end='')
                if not success:
                    print(f"\n{Style.RED}‼️ FAILED: {script_name}{Style.RESET}")
                    failed.append(script)
                else:
                    print(f"\n{Style.GREEN}✓ PASSED: {script_name}{Style.RESET}")
            except Exception as e:
                print(f"\n{'='*60}")
                print(f" {script_name}")
                print(f"{'='*60}")
                print(f"{Style.RED}‼️ {script_name} failed with exception: {e}{Style.RESET}")
                failed.append(script)

    if failed:
        print(f"\n{Style.RED}‼️ {len(failed)} test(s) failed:{Style.RESET}")
        for f in failed:
            print(f"  - {os.path.basename(f)}")
        sys.exit(1)

    # check if there are any diffs between generated refs and committed/staged refs.
    # excludes reference files from other platforms (e.g., _linux files when running on darwin)
    try:
        git_cmd = ['git', '-c', 'core.autocrlf=input', '-c', 'core.safecrlf=false',
                   '--no-pager', 'diff', '--', ref_dir]

        for plat in ALL_PLATFORMS:
            if plat != CURRENT_PLATFORM:
                # exclude reference files from scripts with platform suffix
                # reference files are named like reference_{script_name}_l{lineno}
                # For platform-specific scripts reference_docker-alpine_linux.sh_l11
                git_cmd.append(f':!*_{plat}.sh_*')

        print(f"Running git diff (excluding other platforms): {' '.join(git_cmd)}")
        res = subprocess.run(git_cmd, text=True, encoding='utf-8', capture_output=True, check=False)

        # git diff only checked for changes so far, but we also want to check for untracked files
        untracked_cmd = ['git', 'ls-files', '--others', '--exclude-standard', '--', ref_dir]
        untracked_res = subprocess.run(untracked_cmd, text=True, encoding='utf-8', capture_output=True, check=False)

        # still filter out reference files from other platforms although they should never really
        untracked_files = [f for f in untracked_res.stdout.splitlines()]

        print(res.stdout)
        has_diff = bool(res.stdout)
        has_untracked = bool(untracked_files)

        if has_untracked:
            print("untracked reference files:")
            for f in untracked_files:
                print(f"  {f}")

        if has_diff or has_untracked:
            print("‼️ there are changes in the tests.")
            sys.exit(1)
        else:
            print("✅ no changes detected in reference tests.")

    except subprocess.CalledProcessError as err:
        print(f"‼️‼ an error occurred: {err.stderr}")
        sys.exit(1)

if __name__ == "__main__":
    main()