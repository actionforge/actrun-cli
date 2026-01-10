package nodes

import (
	_ "embed"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"

	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/encoding/unicode/utf32"
)

//go:embed string-encode@v1.yml
var stringEncodeDefinition string

type StringEncode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
}

func (n *StringEncode) OutputValueById(c *core.ExecutionState, outputId core.OutputId) (any, error) {
	input, err := core.InputValueById[string](c, n, ni.Core_string_encode_v1_Input_input)
	if err != nil {
		return nil, err
	}

	op, err := core.InputValueById[string](c, n, ni.Core_string_encode_v1_Input_op)
	if err != nil {
		return nil, err
	}

	var result string
	inputBytes := []byte(input) // The input string is already UTF-8

	switch op {
	case "base64":
		result = base64.StdEncoding.EncodeToString(inputBytes)
	case "base64url":
		result = base64.URLEncoding.EncodeToString(inputBytes)
	case "base32":
		result = base32.StdEncoding.EncodeToString(inputBytes)
	case "hex":
		result = hex.EncodeToString(inputBytes)

	case "utf8":
		result = input // No-op, it's already a UTF-8 string
	case "utf16le":
		encoder := unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM).NewEncoder()
		utf16Bytes, err := encoder.Bytes(inputBytes)
		if err != nil {
			return nil, core.CreateErr(c, err, "failed to encode utf16le")
		}
		result = string(utf16Bytes) // Return raw bytes as a string
	case "utf16be":
		encoder := unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM).NewEncoder()
		utf16Bytes, err := encoder.Bytes(inputBytes)
		if err != nil {
			return nil, core.CreateErr(c, err, "failed to encode utf16be")
		}
		result = string(utf16Bytes) // Return raw bytes as a string
	case "utf32le":
		encoder := utf32.UTF32(utf32.LittleEndian, utf32.IgnoreBOM).NewEncoder()
		utf32Bytes, err := encoder.Bytes(inputBytes)
		if err != nil {
			return nil, core.CreateErr(c, err, "failed to encode utf32le")
		}
		result = string(utf32Bytes) // Return raw bytes as a string
	case "utf32be":
		encoder := utf32.UTF32(utf32.BigEndian, utf32.IgnoreBOM).NewEncoder()
		utf32Bytes, err := encoder.Bytes(inputBytes)
		if err != nil {
			return nil, core.CreateErr(c, err, "failed to encode utf32be")
		}
		result = string(utf32Bytes) // Return raw bytes as a string
	default:
		return nil, core.CreateErr(c, nil, "unknown operation '%s'", op)
	}

	return result, nil
}

func init() {
	err := core.RegisterNodeFactory(stringEncodeDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &StringEncode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
