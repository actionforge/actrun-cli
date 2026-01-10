package nodes

import (
	_ "embed"
	"math/rand"
	"sync"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed random-number@v1.yml
var randomNumberNodeDefinition string

type RandomNumberNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs

	randGenLock sync.Mutex
	randGen     *rand.Rand
}

func (n *RandomNumberNode) OutputValueById(c *core.ExecutionState, outputId core.OutputId) (any, error) {
	min, err := core.InputValueById[float64](c, n, ni.Core_random_number_v1_Input_min)
	if err != nil {
		return nil, err
	}
	max, err := core.InputValueById[float64](c, n, ni.Core_random_number_v1_Input_max)
	if err != nil {
		return nil, err
	}
	seed, err := core.InputValueById[int64](c, n, ni.Core_random_number_v1_Input_seed)
	if err != nil {
		return nil, err
	}

	if seed == -1 {
		seed = generateUniqueSeed()
	}

	n.randGenLock.Lock()
	source := rand.NewSource(seed)
	if n.randGen == nil {
		n.randGen = rand.New(source)
	}
	f := n.randGen.Float64()
	n.randGenLock.Unlock()

	randomNumber := min + f*(max-min)
	return randomNumber, nil
}

func init() {
	err := core.RegisterNodeFactory(randomNumberNodeDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &RandomNumberNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
