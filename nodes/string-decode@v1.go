package nodes

import (
	_ "embed"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"

	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/encoding/unicode/utf32"
)

//go:embed string-decode@v1.yml
var stringDecodeDefinition string

type StringDecode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
}

func (n *StringDecode) OutputValueById(c *core.ExecutionState, outputId core.OutputId) (any, error) {
	input, err := core.InputValueById[string](c, n, ni.Core_string_decode_v1_Input_input)
	if err != nil {
		return nil, err
	}

	op, err := core.InputValueById[string](c, n, ni.Core_string_decode_v1_Input_op)
	if err != nil {
		return nil, err
	}

	var result string
	inputBytes := []byte(input)

	switch op {
	case "base64":
		decoded, err := base64.StdEncoding.DecodeString(input)
		if err != nil {
			return nil, core.CreateErr(c, err, "failed to decode base64")
		}
		result = string(decoded)
	case "base64url":
		decoded, err := base64.URLEncoding.DecodeString(input)
		if err != nil {
			return nil, core.CreateErr(c, err, "failed to decode base64url")
		}
		result = string(decoded)
	case "base32":
		decoded, err := base32.StdEncoding.DecodeString(input)
		if err != nil {
			return nil, core.CreateErr(c, err, "failed to decode base32")
		}
		result = string(decoded)
	case "hex":
		decoded, err := hex.DecodeString(input)
		if err != nil {
			return nil, core.CreateErr(c, err, "failed to decode hex")
		}
		result = string(decoded)

	case "utf8":
		result = input // No-op, it's already a UTF-8 string
	case "utf16le":
		decoder := unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM).NewDecoder()
		utf8Bytes, err := decoder.Bytes(inputBytes)
		if err != nil {
			return nil, core.CreateErr(c, err, "failed to decode utf16le")
		}
		result = string(utf8Bytes)
	case "utf16be":
		decoder := unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM).NewDecoder()
		utf8Bytes, err := decoder.Bytes(inputBytes)
		if err != nil {
			return nil, core.CreateErr(c, err, "failed to decode utf16be")
		}
		result = string(utf8Bytes)
	case "utf32le":
		decoder := utf32.UTF32(utf32.LittleEndian, utf32.IgnoreBOM).NewDecoder()
		utf8Bytes, err := decoder.Bytes(inputBytes)
		if err != nil {
			return nil, core.CreateErr(c, err, "failed to decode utf32le")
		}
		result = string(utf8Bytes)
	case "utf32be":
		decoder := utf32.UTF32(utf32.BigEndian, utf32.IgnoreBOM).NewDecoder()
		utf8Bytes, err := decoder.Bytes(inputBytes)
		if err != nil {
			return nil, core.CreateErr(c, err, "failed to decode utf32be")
		}
		result = string(utf8Bytes)

	case "html":
		result = html.UnescapeString(input)
	case "url":
		decoded, err := url.QueryUnescape(input)
		if err != nil {
			return nil, core.CreateErr(c, err, "failed to decode url")
		}
		result = decoded
	case "urlpath":
		decoded, err := url.PathUnescape(input)
		if err != nil {
			return nil, core.CreateErr(c, err, "failed to decode url path")
		}
		result = decoded
	case "json":
		result = unescapeJSON(input)
	case "xml":
		result = unescapeXML(input)
	default:
		return nil, core.CreateErr(c, nil, "unknown operation '%s'", op)
	}

	return result, nil
}

var jsonEscapeRegex = regexp.MustCompile(`\\(["\\bfnrt/]|u[0-9a-fA-F]{4})`)

func unescapeJSON(s string) string {
	return jsonEscapeRegex.ReplaceAllStringFunc(s, func(match string) string {
		switch match {
		case `\"`:
			return `"`
		case `\\`:
			return `\`
		case `\b`:
			return "\b"
		case `\f`:
			return "\f"
		case `\n`:
			return "\n"
		case `\r`:
			return "\r"
		case `\t`:
			return "\t"
		case `\/`:
			return "/"
		default:
			// Handle \uXXXX
			if strings.HasPrefix(match, `\u`) && len(match) == 6 {
				code, err := strconv.ParseInt(match[2:], 16, 32)
				if err == nil {
					return string(rune(code))
				}
			}
			return match
		}
	})
}

func unescapeXML(s string) string {
	replacements := []struct {
		escaped   string
		unescaped string
	}{
		{"&lt;", "<"},
		{"&gt;", ">"},
		{"&amp;", "&"},
		{"&apos;", "'"},
		{"&quot;", `"`},
	}

	result := s
	for _, r := range replacements {
		result = strings.ReplaceAll(result, r.escaped, r.unescaped)
	}

	// Handle numeric character references like &#60; or &#x3C;
	numericRegex := regexp.MustCompile(`&#(x[0-9a-fA-F]+|\d+);`)
	result = numericRegex.ReplaceAllStringFunc(result, func(match string) string {
		inner := match[2 : len(match)-1]
		var code int64
		var err error
		if strings.HasPrefix(inner, "x") {
			code, err = strconv.ParseInt(inner[1:], 16, 32)
		} else {
			code, err = strconv.ParseInt(inner, 10, 32)
		}
		if err == nil {
			return string(rune(code))
		}
		return match
	})

	return result
}

func init() {
	err := core.RegisterNodeFactory(stringDecodeDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool, opts core.RunOpts) (core.NodeBaseInterface, []error) {
		return &StringDecode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
