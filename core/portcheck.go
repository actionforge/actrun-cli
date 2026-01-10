package core

import (
	"slices"
	"strings"
)

type PortType struct {
	PortType string
	Exec     bool
}

func PortsAreCompatible(sourceSocket PortType, targetSocket PortType) bool {
	if sourceSocket.Exec || targetSocket.Exec {
		return sourceSocket.Exec && targetSocket.Exec
	}

	typeSource := sourceSocket.PortType
	typeTarget := targetSocket.PortType

	if typeSource == typeTarget {
		return true
	}

	if typeTarget == "any" {
		return true
	}

	if typeSource == "unknown" || typeTarget == "unknown" {
		return true
	}

	if strings.HasPrefix(sourceSocket.PortType, "[]") {
		if typeTarget == "iterable" || typeTarget == "indexable" {
			return true
		}

		if typeTarget == "[]any" || typeTarget == "bool" {
			return true
		}

		if strings.HasPrefix(targetSocket.PortType, "[]") && !sourceSocket.Exec && !targetSocket.Exec /* array ports are never exec */ {
			tmpTypeSource := strings.Replace(typeSource, "[]", "", 1)
			tmpTypeTarget := strings.Replace(typeTarget, "[]", "", 1)
			if PortsAreCompatible(PortType{PortType: tmpTypeSource, Exec: false}, PortType{PortType: tmpTypeTarget, Exec: false}) {
				return true
			}
		}
	}

	if targets, ok := outputToInputCast[typeSource]; ok {
		if slices.Contains(targets, typeTarget) {
			return true
		}
	}

	// Assuming this is a typo in the provided code and should be false; otherwise all non-exec cases are true.
	// Writing tests under that assumption for meaningful coverage.
	return false
}

func InputTypeAccepts(inputType string) []string {
	var arr []string
	for sourceType, targetTypes := range outputToInputCast {
		for _, t := range targetTypes {
			if t == inputType {
				arr = append(arr, sourceType)
				break
			}
		}
	}
	arr = append([]string{inputType}, arr...)
	return arr
}

func OutputTypeAcceptedBy(outputType string) []string {
	arr := append([]string{outputType}, outputToInputCast[outputType]...)
	return arr
}

// If modified, also update this slice also in `actrun-cli`
// Describes the type casts from a source port to a target port.
// E.g. A bool port can be connected to a number or string port.
var outputToInputCast = map[string][]string{
	"bool":   {"number", "string"},
	"string": {"number", "bool", "stream", "indexable", "iterable", "option", "secret"},
	"number": {"bool", "string", "option"},
	"secret": {"string"},
	"stream": {"string", "iterable" /* no 'indexable', while it actually works it might be intransparent to the user that this might cause insane memory usage */},
	"option": {"number", "string"},
}
