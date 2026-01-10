package nodes

import (
	crypto_rand "crypto/rand"
	_ "embed"
	"encoding/binary"
	"io"
	"math/rand"
	"strings"
	"time"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed random-stream@v1.yml
var randomStreamNodeDefinition string

type RandomStreamNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
}

type RandomStreamReader struct {
	length            int
	includeNumbers    bool
	includeCharacters bool
	includeUppercase  bool
	includeSpecial    bool
	seed              int64
	rng               *rand.Rand
	characters        string
	count             int
}

func NewRandomStringReader(length int, includeNumbers, includeCharacters, includeUppercase, includeSpecial bool, seed int64) *RandomStreamReader {
	numbers := "0123456789"
	lowercase := "abcdefghijklmnopqrstuvwxyz"
	uppercase := "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	special := "!@#$%^&*()-_=+[]{}|;:'\",.<>?/\\`~"

	var characterPool strings.Builder
	if includeNumbers {
		characterPool.WriteString(numbers)
	}
	if includeCharacters {
		characterPool.WriteString(lowercase)
	}
	if includeUppercase {
		characterPool.WriteString(uppercase)
	}
	if includeSpecial {
		characterPool.WriteString(special)
	}

	if characterPool.Len() == 0 {
		panic("no character sets selected")
	}

	if seed == 0 {
		seed = generateUniqueSeed()
	}

	rng := rand.New(rand.NewSource(seed))

	return &RandomStreamReader{
		length:            length,
		includeNumbers:    includeNumbers,
		includeCharacters: includeCharacters,
		includeUppercase:  includeUppercase,
		includeSpecial:    includeSpecial,
		seed:              seed,
		rng:               rng,
		characters:        characterPool.String(),
		count:             0,
	}
}

func (r *RandomStreamReader) Read(p []byte) (n int, err error) {
	if r.count >= r.length {
		return 0, io.EOF
	}

	for i := range p {
		if r.count >= r.length {
			return i, io.EOF
		}
		randomIndex := r.rng.Intn(len(r.characters))
		p[i] = r.characters[randomIndex]
		r.count++
	}
	return len(p), nil
}

func (n *RandomStreamNode) OutputValueById(c *core.ExecutionState, outputId core.OutputId) (any, error) {
	length, err := core.InputValueById[int](c, n, ni.Core_random_stream_v1_Input_length)
	if err != nil {
		return nil, err
	}
	includeNumbers, err := core.InputValueById[bool](c, n, ni.Core_random_stream_v1_Input_include_numbers)
	if err != nil {
		return nil, err
	}
	includeCharacters, err := core.InputValueById[bool](c, n, ni.Core_random_stream_v1_Input_include_characters)
	if err != nil {
		return nil, err
	}
	includeUppercase, err := core.InputValueById[bool](c, n, ni.Core_random_stream_v1_Input_include_uppercase)
	if err != nil {
		return nil, err
	}
	includeSpecial, err := core.InputValueById[bool](c, n, ni.Core_random_stream_v1_Input_include_special)
	if err != nil {
		return nil, err
	}
	seed, err := core.InputValueById[int64](c, n, ni.Core_random_stream_v1_Input_seed)
	if err != nil {
		return nil, err
	}

	reader := NewRandomStringReader(length, includeNumbers, includeCharacters, includeUppercase, includeSpecial, seed)
	return core.DataStreamFactory{
		Reader: reader,
	}, nil
}

func init() {
	err := core.RegisterNodeFactory(randomStreamNodeDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &RandomStreamNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}

func generateUniqueSeed() int64 {
	nanoTime := time.Now().UnixNano()
	cryptoRand := make([]byte, 8)
	_, err := crypto_rand.Read(cryptoRand)
	if err != nil {
		panic(err)
	}
	cryptoRandInt := int64(binary.BigEndian.Uint64(cryptoRand))
	seed := nanoTime ^ cryptoRandInt
	return seed
}
