#!/usr/bin/env python
# -*- coding: utf-8 -*-

"""
Runs all .sh files in "tests_e2e/scripts".
These scripts contain "#! test <command>" comments.
They are expanded, executed and then diffed against the output in
"tests/integrations/references".
"""

import os
import subprocess
import sys
import shutil
import shlex
import re
import platform
import tempfile
from pathlib import Path

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
    return [str(p) for p in Path(directory).rglob("*.sh")]

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

    subprocess.run(
        ["bash", to_posix_path(script_file)],
        shell=IS_WINDOWS,
        env=env,
        cwd=working_dir,
        stdout=sys.stdout,
        stderr=subprocess.STDOUT,
        check=True
    )

def process_and_run_test(root_dir: str, source_script: str, ref_dir: str, cov_dir: str):
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
                
                # write the execution command piped to redaction and reference file
                dest.write(f"{test_cmd} 2>&1 | redact_abs_paths | tr -d '\r' | tee {ref_file}\n")
            
            else:
                # here are all the other non test lines
                stripped = line.strip()
                if stripped:
                    if stripped.startswith("function"):
                        fname = stripped.split()[1] if len(stripped.split()) > 1 else "unknown"
                        print(f"‼️ 'function' keyword is not POSIX compliant. Use '{fname}() {{' instead.")
                        sys.exit(1)
                    
                    if not current_func_name and stripped.endswith("() {"):
                        current_func_name = stripped.split()[0]
                    elif stripped == "}":
                        if not current_func_name:
                            print(f"‼️ Closing brace without function definition in {source_script}:{lineno}")
                            sys.exit(1)
                        current_func_name = None
                    elif not stripped.startswith("#"):
                        # echo line if not inside a function definition
                        if not current_func_name:
                            dest.write(f"echo {Style.BLUE}L{lineno} $ {shlex.quote(stripped)}{Style.RESET}\n")
                
                dest.write(line)

        if current_func_name:
            print(f"‼️ Function {current_func_name} was never closed.")
            sys.exit(1)

    tmp_cwd = tempfile.mkdtemp(prefix=f"actrun.{script_name}")
    print(f"Running script: {source_script} -> {temp_script_path}:\n           cwd: {tmp_cwd}\n")
    run_test_script(root_dir, temp_script_path, tmp_cwd)

def compile_binaries(is_github_runner: bool):
    if is_github_runner:
        return

    # build CLI
    cli_out = 'dist/actrun' + ('.exe' if IS_WINDOWS else '')
    
    env = GLOBAL_ENVS.copy()
    env["GCFLAGS"] = "-N -l"
    
    build_cmd = ['go', 'build', '-o', cli_out, '.']
    if COVERAGE:
        # TODO: (Seb) coverage build takes ages
        build_cmd = ['go', 'test', '.', '-buildvcs=true', '-cover', '-coverprofile', '-tags=main_test', '-c', '-o', cli_out]
    
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

    # delete all refs if running full suite
    if target_test is None:
        shutil.rmtree(ref_dir, ignore_errors=True)
        os.makedirs(ref_dir, exist_ok=True)

    compile_binaries(is_gh_actions)
    
    GLOBAL_ENVS['ACT_SHARED_LIB_PATH'] = get_shared_lib_path(is_gh_actions)

    # Run Tests
    if target_test is None:
        for script_path in collect_shell_scripts(scripts_dir):
            process_and_run_test(base_cwd, script_path, ref_dir, cov_dir)
    else:
        full_path = os.path.join(scripts_dir, target_test)
        process_and_run_test(base_cwd, full_path, ref_dir, cov_dir)

    # check if there are any diffs between generated refs and committed/staged refs
    try:
        git_cmd = ['git', '-c', 'core.autocrlf=input', '-c', 'core.safecrlf=false', 
                   '--no-pager', 'diff', ref_dir]
        
        res = subprocess.run(git_cmd, text=True, encoding='utf-8', capture_output=True, check=False)

        print(res.stdout)
        if res.stdout:
            print("‼️ there are changes in the tests.")
            sys.exit(1)
        else:
            print("✅ no changes detected in reference tests.")

    except subprocess.CalledProcessError as err:
        print(f"‼️‼ an error occurred: {err.stderr}")
        sys.exit(1)

if __name__ == "__main__":
    main()