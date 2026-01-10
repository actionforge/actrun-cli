package nodes

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	_ "embed"
	"encoding/hex"
	"hash"
	"hash/crc32"
	"io"
	"strings"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
	"github.com/actionforge/actrun-cli/utils"

	"golang.org/x/crypto/blake2b"
	"golang.org/x/crypto/sha3"
)

//go:embed hash@v1.yml
var hashDefinition string

type HashNode struct {
	core.NodeBaseComponent
	core.Executions
	core.Inputs
	core.Outputs
}

func getHashFunction(c *core.ExecutionState, algorithm string) (hash.Hash, error) {
	switch strings.ToLower(algorithm) {
	case "sha1":
		return sha1.New(), nil
	case "sha224":
		return sha256.New224(), nil
	case "sha256":
		return sha256.New(), nil
	case "sha384":
		return sha512.New384(), nil
	case "sha512":
		return sha512.New(), nil
	case "sha3_256":
		return sha3.New256(), nil
	case "sha3_384":
		return sha3.New384(), nil
	case "sha3_512":
		return sha3.New512(), nil
	case "md5":
		return md5.New(), nil
	case "crc32":
		return crc32.New(crc32.MakeTable(crc32.IEEE)), nil
	case "blake2b256":
		return blake2b.New256(nil)
	case "blake2b384":
		return blake2b.New384(nil)
	case "blake2b512":
		return blake2b.New512(nil)
	default:
		return nil, core.CreateErr(c, nil, "unsupported hash algorithm: %s", algorithm)
	}
}

func (n *HashNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	input, err := core.InputValueById[io.Reader](c, n, ni.Core_hash_v1_Input_input)
	if err != nil {
		return err
	}

	defer utils.SafeCloseReaderAndIgnoreError(input)

	algorithm, err := core.InputValueById[string](c, n, ni.Core_hash_v1_Input_algorithm)
	if err != nil {
		return err
	}

	hashFunc, err := getHashFunction(c, algorithm)
	if err != nil {
		return err
	}

	_, copyErr := io.Copy(hashFunc, input)
	if copyErr != nil {
		copyErr = core.CreateErr(c, copyErr, "error reading stream")
	}

	// Ensure the input reader is closed in all cases.
	// If closing the reader fails without a prior error,
	// treat it as an error which is part of the copy op.
	err = utils.SafeCloseReader(input)
	if err != nil && copyErr == nil {
		copyErr = err
	}

	hashString := hex.EncodeToString(hashFunc.Sum(nil))
	err = n.Outputs.SetOutputValue(c, ni.Core_hash_v1_Output_hash, hashString, core.SetOutputValueOpts{})
	if err != nil {
		return err
	}

	if copyErr != nil {
		err := n.Execute(ni.Core_hash_v1_Output_exec_err, c, copyErr)
		if err != nil {
			return err
		}
	} else {
		err := n.Execute(ni.Core_hash_v1_Output_exec_success, c, nil)
		if err != nil {
			return err
		}
	}

	return nil
}

func init() {
	err := core.RegisterNodeFactory(hashDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &HashNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
