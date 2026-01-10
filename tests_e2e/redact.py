import re
import sys

def redact_paths(text):
    script_pattern = re.compile(r'run-script-\d+')
    
    posix_pattern = re.compile(r'''([\\\/\s\t'":~.@$]|^)(\/[^'"\s]+)(['"]?)''')
    windows_pattern = re.compile(r"[a-zA-Z]:\\(?:[^\\/:*?\"<>|\r\n]+\\)*([^\\/:*?\"<>|\r\n]+)")

    def replace_windows_path(match):
        path = match.group(0)
        filename = re.split(r'[\\\/]', path)[-1]
        return f"[REDACTED]/{filename}"

    def replace_posix_path(match):
        prefix, path, suffix = match.groups()
        filename = path.split('/')[-1]
        return f"{prefix}[REDACTED]/{filename}{suffix}"

    # redact run-script-[0-9]*. This is to stabilize test scripts
    # that intentionally fail in the Python node
    text = script_pattern.sub("[REDACTED_SCRIPT]", text)
    
    text = windows_pattern.sub(replace_windows_path, text)
    text = posix_pattern.sub(replace_posix_path, text)
    
    return text

if __name__ == '__main__':
    input_text = sys.stdin.read()
    print(redact_paths(input_text), end='')