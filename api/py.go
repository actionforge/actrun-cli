//go:build cpython

package api

/*
#include <stdlib.h> // for free
#include "py.h"
*/
import "C"
import (
	"fmt"
	"unsafe"

	"github.com/actionforge/actrun-cli/core"
)

// https://github.com/python/cpython/blob/6f4d64b048133c60d40705fb5ef776f78c7dd710/Include/compile.h#L9
const Py_file_input = 257

//export InitPython
func InitPython(lookup_func_cb unsafe.Pointer) C.int {
	return C.store_lookup_callback(lookup_func_cb)
}

//export RunPythonCode
func RunPythonCode(code *C.char) {
	cCode := C.GoString(code)

	res, err := RunPythonCodeRet(cCode)
	if err == nil {
		fmt.Println("Result:", res)
	}
}

func RunPythonCodeRet(code string) (any, error) {
	cCode := C.CString(code)
	defer C.free(unsafe.Pointer(cCode))

	globals := C.C_PyDict_New()
	locals := globals

	_ = C.C_PyRun_String(cCode, C.int(Py_file_input), globals, locals)

	if C.C_PyErr_Occurred() != nil {
		C.C_PyErr_Print()
		return nil, fmt.Errorf("an error occurred while executing the Python code")
	}

	mainFunc := C.C_PyDict_GetItemString(locals, C.CString("main"))
	if mainFunc != nil {
		result := C.C_PyObject_CallObject(mainFunc, nil)
		if C.C_PyErr_Occurred() != nil {
			C.C_PyErr_Print()
			return nil, fmt.Errorf("an error occurred while calling the 'main' function")
		}

		if result != nil {
			switch {
			case C.C_IsBool(result) != 0:
				return C.C_PyObject_IsTrue(result) != 0, nil
			case C.C_IsLong(result) != 0:
				return int(C.C_PyLong_AsLong(result)), nil
			case C.C_IsFloat(result) != 0:
				return float64(C.C_PyFloat_AsDouble(result)), nil
			case C.C_IsUtf8String(result) != 0:
				strResult := C.C_PyUnicode_AsUTF8(result)
				if C.C_PyErr_Occurred() != nil {
					C.C_PyErr_Print()
					return nil, fmt.Errorf("an error occurred while converting the result to a string")
				}
				return C.GoString(strResult), nil
			default:
				name := C.C_PyType_GetName(result)
				return nil, core.CreateErr(nil, nil, "unsupported return type: %v", C.GoString(name)).SetHint("only 'bool', 'float', 'int' and 'str' can be returned")
			}
		}
	}

	// if no `main` exists or no value is returned
	// TODO: (Seb) guess if this is noop or actually an error
	return nil, nil
}
