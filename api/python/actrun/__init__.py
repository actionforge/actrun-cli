import sys
import ctypes
import os
import platform
import subprocess
from ctypes.util import find_library
from ctypes import c_char_p, c_void_p, c_int, POINTER

def _load_lib_path():

    lib_suffix = {
        'linux': 'so',
        'darwin': 'so',
        'win32': 'dll'
    }.get(sys.platform, None)

    CURRENT_OS = {
        "darwin": "macos",
        "windows": "windows",
        "linux": "linux"
    }.get(platform.system().lower(), None)

    CURRENT_ARCH = {
        "x86_64": "x64",
        "amd64": "x64",
        "arm64": "arm64",
        "aarch64": "arm64"
    }.get(platform.machine().lower(), None)

    if not lib_suffix:
        raise RuntimeError(f"unsupported platform: {sys.platform}")

    if not CURRENT_OS:
        raise RuntimeError(f"unsupported platform: {platform.system()}")

    if not CURRENT_ARCH:
        raise RuntimeError(f"unsupported architecture: {platform.machine()}")

    lp = os.getenv('ACT_SHARED_LIB_PATH')
    if not lp:
        lp = find_library(f'actrun.${lib_suffix}')

    if not lp:
        lp = os.path.join(os.path.dirname(__file__), f'actrun-py-{CURRENT_OS}-{CURRENT_ARCH}.{lib_suffix}')

    if not lp or not os.path.exists(lp):
        raise FileNotFoundError(f"the specified shared library path does not exist: {lp}")

    if CURRENT_OS == "macos":
        try:
            # as long as the library is not distributed via pip, we need to remove the quarantine attribute
            subprocess.run(['xattr', '-d', 'com.apple.quarantine', lp], check=False, stderr=subprocess.DEVNULL)
        except subprocess.CalledProcessError:
            pass

    return lp

lib_path = _load_lib_path()

try:
    lib = ctypes.PyDLL(lib_path)
except Exception as e:
    raise RuntimeError(f"failed to load the shared library at path {lib_path}") from e

# Private function that is used by 'actrun'
# to retrieve function pointers from the Python library.
def _lookup_func_name(name):
    try:
        func = getattr(ctypes.pythonapi, name.decode("ascii"))
        func_ptr = ctypes.cast(func, c_void_p)
        return func_ptr.value
    except (AttributeError, UnicodeDecodeError):
        return c_void_p(0).value

class _GoLogger:
    def __init__(self, callback):
        self.callback = callback

    def write(self, message):
        self.callback(c_char_p(message.encode('utf-8')))

    # for python 3 needed
    def flush(self):
        pass


def _hook_setup():
    CALLBACK_FUNC_TYPE = ctypes.CFUNCTYPE(c_void_p, c_char_p)
    # Ensure _lookup_func_name is defined in your scope
    lookup_func_cb = CALLBACK_FUNC_TYPE(_lookup_func_name) 

    lib.InitPython.argtypes = [c_void_p]
    lib.InitPython.restype = c_int
    if lib.InitPython(lookup_func_cb) != 0:
        return

    lib.StdoutCallback.argtypes = [c_char_p]
    sys.__stdout__ = sys.stdout
    sys.__stderr__ = sys.stderr
    sys.stdout = _GoLogger(lib.StdoutCallback)
    sys.stderr = _GoLogger(lib.StdoutCallback)

    # 1. graphName (char*)
    # 2. contentPtr (char*)
    # 3. contentLen (int)
    # 4. secretKeys (char**)
    # 5. secretValues (char**)
    # 6. secretCount (int)
    # 7. inputKeys (char**)
    # 8. inputValues (char**)
    # 9. inputCount (int)
    # 10. args (char**)
    # 11. argCount (int)
    lib.RunGraph.argtypes = [
        c_char_p,           
        c_char_p, c_int,    
        POINTER(c_char_p), POINTER(c_char_p), c_int, 
        POINTER(c_char_p), POINTER(c_char_p), c_int, 
        POINTER(c_char_p), c_int            
    ]
    lib.RunGraph.restype = c_int

    lib.RunPythonCode.argtypes = [c_char_p]
    lib.RunPythonCode.restype = c_int


def _dict_to_c_arrays(data: dict):
    """Helper to convert python dict to two C arrays (keys, values)"""
    if not data:
        return None, None, 0
    
    count = len(data)
    keys_arr = (c_char_p * count)()
    vals_arr = (c_char_p * count)()
    
    for i, (k, v) in enumerate(data.items()):
        keys_arr[i] = str(k).encode('utf-8')
        vals_arr[i] = str(v).encode('utf-8')
        
    return keys_arr, vals_arr, count

def _list_to_c_array(data: list):
    """Helper to convert python list to C array"""
    if not data:
        return None, 0
    
    count = len(data)
    arr = (c_char_p * count)()
    for i, v in enumerate(data):
        arr[i] = str(v).encode('utf-8')
        
    return arr, count

def run_graph(graph_name: str, graph_path: str, secrets: dict = None, inputs: dict = None, args: list = None) -> bool:
    """
    Reads the graph content from the provided path and executes it.
    """

    if not os.path.exists(graph_path):
        raise FileNotFoundError(f"graph file not found at: {graph_path}")
    
    with open(graph_path, "rb") as f:
        graph_content = f.read()

    if secrets is None:
        secrets = {}
    if inputs is None:
        inputs = {}
    if args is None:
        args = []

    c_name = graph_name.encode('utf-8')
    c_content = graph_content
    c_content_len = len(graph_content)
    
    s_keys, s_vals, s_count = _dict_to_c_arrays(secrets)
    i_keys, i_vals, i_count = _dict_to_c_arrays(inputs)
    a_arr, a_count = _list_to_c_array(args)

    result = lib.RunGraph(
        c_name,
        c_content, c_content_len,
        s_keys, s_vals, s_count,
        i_keys, i_vals, i_count,
        a_arr, a_count
    )
    
    return result == 0


_hook_setup()
