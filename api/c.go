//go:build api

package api

import (
	"C"
	"context"
	"fmt"
	"unsafe"

	"github.com/actionforge/actrun-cli/core"
	"github.com/actionforge/actrun-cli/utils"
)

//export StdoutCallback
func StdoutCallback(o *C.char) {
	og := C.GoString(o)
	utils.LogOut.Out.Write([]byte(og))
}

// Helper to convert C array of strings to Go slice
func cArrayToGoSlice(arr **C.char, len C.int) []string {
	if len == 0 {
		return []string{}
	}
	tmpslice := unsafe.Slice(arr, int(len))
	goSlice := make([]string, 0, len)
	for _, s := range tmpslice {
		goSlice = append(goSlice, C.GoString(s))
	}
	return goSlice
}

// Helper to convert C arrays (keys/values) to Go map
func cArraysToGoMap(keys **C.char, values **C.char, len C.int) map[string]string {
	m := make(map[string]string)
	if len == 0 {
		return m
	}
	kSlice := unsafe.Slice(keys, int(len))
	vSlice := unsafe.Slice(values, int(len))

	for i := 0; i < int(len); i++ {
		m[C.GoString(kSlice[i])] = C.GoString(vSlice[i])
	}
	return m
}

//export RunGraph
func RunGraph(
	graphName *C.char,
	contentPtr *C.char, contentLen C.int,
	secretKeys **C.char, secretValues **C.char, secretCount C.int,
	inputKeys **C.char, inputValues **C.char, inputCount C.int,
	args **C.char, argCount C.int,
) C.int {
	name := C.GoString(graphName)

	content := C.GoBytes(unsafe.Pointer(contentPtr), contentLen)

	secrets := cArraysToGoMap(secretKeys, secretValues, secretCount)
	inputs := cArraysToGoMap(inputKeys, inputValues, inputCount)
	// convert inputs map[string]string to map[string]any
	inputsAny := make(map[string]any, len(inputs))
	for k, v := range inputs {
		inputsAny[k] = v
	}

	goArgs := cArrayToGoSlice(args, argCount)

	err := core.RunGraph(context.Background(), name, content, core.RunOpts{
		OverrideSecrets: secrets,
		OverrideInputs:  inputsAny,
		Args:            goArgs,
	}, nil)
	if err != nil {
		fmt.Printf("%v", err)
		return -1
	}
	return 0
}
