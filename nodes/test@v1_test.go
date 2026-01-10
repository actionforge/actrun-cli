//go:build integration_tests

package nodes

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	// initialize all nodes
	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
	"github.com/actionforge/actrun-cli/utils"

	"go.yaml.in/yaml/v4"
)

// Test that the node type exists.
func Test_NewNodeInstance_Exists(t *testing.T) {
	n, err := core.NewNodeInstance("core/run@v1", nil, "", nil, false)
	if err != nil {
		t.Fatal(err)
	}

	if n.GetNodeTypeId() != "core/run@v1" {
		t.Errorf("Expected node name to be 'core/run@v1', got '%s'", n.GetNodeTypeId())
	}
}

// Test that the node type doesn't exist
func Test_NewNodeInstance_NotExists(t *testing.T) {
	_, err := core.NewNodeInstance("abc@v2", nil, "", nil, false)
	if err == nil {
		t.Error("expected error")
		return
	}
}

// Test that connects two nodes, and then requests the output value from the first one.
// The value of the output value is correctly set, but in this test the requested type
// will require a cast as the node requests a different type.
// E.g:
//   - the requested type is int32, so an implicit cast from bool to int32 must occur.
func Test_InputValueById_Casting(t *testing.T) {

	type expectError struct {
		loading  bool
		setValue bool
		getValue bool
	}

	SAME_AS_OUTPUT := core.OutputId("")

	tests := []struct {
		output any // the value for the output port

		// *setOnOutput* is the output of the node that should receive the value.
		// *setOnPort* is the actual port. They are usually the same, or different
		// for arrays since the output receives the array, and the port only spits
		// out the actual item at a given index.
		setOnOutput core.OutputId
		setOnPort   core.OutputId

		inputId     core.InputId // the input port to connect to
		expected    any          // the expected value on the input port
		expectError expectError  // whether to expect an error
	}{
		{true, ni.Core_test_v1_Output_output_bool_foo123, SAME_AS_OUTPUT, ni.Core_test_v1_Input_input_bool_foo123, int8(1), expectError{}},
		{true, ni.Core_test_v1_Output_output_bool_foo123, SAME_AS_OUTPUT, ni.Core_test_v1_Input_input_bool_foo123, int32(1), expectError{}},
		{true, ni.Core_test_v1_Output_output_bool_foo123, SAME_AS_OUTPUT, ni.Core_test_v1_Input_input_bool_foo123, int64(1), expectError{}},
		{true, ni.Core_test_v1_Output_output_bool_foo123, SAME_AS_OUTPUT, ni.Core_test_v1_Input_input_bool_foo123, string("true"), expectError{}},
		{int(4), ni.Core_test_v1_Output_output_number_foo123, SAME_AS_OUTPUT, ni.Core_test_v1_Input_input_number_foo123, "4", expectError{}},
		{int8(42), ni.Core_test_v1_Output_output_number_foo123, SAME_AS_OUTPUT, ni.Core_test_v1_Input_input_number_foo123, true, expectError{}},
		{int8(42), ni.Core_test_v1_Output_output_number_foo123, SAME_AS_OUTPUT, ni.Core_test_v1_Input_input_number_foo123, int32(42), expectError{}},
		{int32(420), ni.Core_test_v1_Output_output_number_foo123, SAME_AS_OUTPUT, ni.Core_test_v1_Input_input_number_foo123, int32(420), expectError{}},
		{int64(4200000), ni.Core_test_v1_Output_output_number_foo123, SAME_AS_OUTPUT, ni.Core_test_v1_Input_input_number_foo123, int32(4200000), expectError{}},
		{int64(4200000), ni.Core_test_v1_Output_output_number_foo123, SAME_AS_OUTPUT, ni.Core_test_v1_Input_input_number_foo123, string("4200000"), expectError{}},
		{[]bool{true, false, true}, ni.Core_test_v1_Output_output_array_bool_foo123, SAME_AS_OUTPUT, ni.Core_test_v1_Input_input_array_bool_foo123, []int32{1, 0, 1}, expectError{}},
		{[]bool{true, false, true}, ni.Core_test_v1_Output_output_array_bool_foo123, SAME_AS_OUTPUT, ni.Core_test_v1_Input_input_array_bool_foo123, []string{"true", "false", "true"}, expectError{}},
		{[]int16{math.MinInt16, -1, 0, 1, 4, math.MaxInt16}, ni.Core_test_v1_Output_output_array_number_foo123, SAME_AS_OUTPUT, ni.Core_test_v1_Input_input_array_number_foo123, []int16{math.MinInt16, -1, 0, 1, 4, math.MaxInt16}, expectError{}},
		{[]int16{math.MinInt16, -1, 0, 1, 4, math.MaxInt16}, ni.Core_test_v1_Output_output_array_number_foo123, SAME_AS_OUTPUT, ni.Core_test_v1_Input_input_array_number_foo123, []string{"-32768", "-1", "0", "1", "4", "32767"}, expectError{}},
		{[]int32{math.MinInt32, -1, 0, 1, 4, math.MaxInt32}, ni.Core_test_v1_Output_output_array_number_foo123, SAME_AS_OUTPUT, ni.Core_test_v1_Input_input_array_number_foo123, []bool{true, true, false, true, true, true}, expectError{}},
		{[]int32{math.MinInt32, -1, 0, 1, 4, math.MaxInt32}, ni.Core_test_v1_Output_output_array_number_foo123, SAME_AS_OUTPUT, ni.Core_test_v1_Input_input_array_number_foo123, []string{"-2147483648", "-1", "0", "1", "4", "2147483647"}, expectError{}},
		{[]uint64{0, 1, 4, math.MaxUint64}, ni.Core_test_v1_Output_output_array_number_foo123, SAME_AS_OUTPUT, ni.Core_test_v1_Input_input_array_number_foo123, []string{"0", "1", "4", "18446744073709551615"}, expectError{}},
		{[]float64{-1, 0, 1, 4, math.MaxFloat64}, ni.Core_test_v1_Output_output_array_number_foo123, SAME_AS_OUTPUT, ni.Core_test_v1_Input_input_array_number_foo123, []string{"-1", "0", "1", "4", strconv.FormatFloat(math.MaxFloat64, 'f', -1, 64)}, expectError{}},

		// test stream ports
		{core.DataStreamFactory{
			Reader: strings.NewReader("hello"),
		}, ni.Core_test_v1_Output_output_stream_foo123, SAME_AS_OUTPUT, ni.Core_test_v1_Input_input_stream_foo123, core.DataStreamFactory{}, expectError{}},
		{"hello", ni.Core_test_v1_Output_output_string_foo123, SAME_AS_OUTPUT, ni.Core_test_v1_Input_input_stream_foo123, strings.NewReader(""), expectError{}},
		{"hello", ni.Core_test_v1_Output_output_string_foo123, SAME_AS_OUTPUT, ni.Core_test_v1_Input_input_stream_foo123, core.DataStreamFactory{}, expectError{}},

		// test storage-provider
		{DummyStorageProvider{}, ni.Core_test_v1_Output_output_storage_provider_foo123, SAME_AS_OUTPUT, ni.Core_test_v1_Input_input_storage_provider_foo123, DummyStorageProvider{}, expectError{}},

		// connect two array ports
		{[]string{"foo", "bar", "bas"}, ni.Core_test_v1_Output_output_array_string_foo123, SAME_AS_OUTPUT, ni.Core_test_v1_Input_input_array_string_foo123, []string{"foo", "bar", "bas"}, expectError{}},

		// connect two index ports
		{[]string{"abc", "def", "ghi"}, ni.Core_test_v1_Output_output_array_string_foo123, ni.Core_test_v1_Output_output_array_string_foo123 + "[0]", ni.Core_test_v1_Input_input_array_string_foo123 + "[0]", "abc", expectError{}},

		// connect a string output with an index port
		{"hello", ni.Core_test_v1_Output_output_string_foo123, SAME_AS_OUTPUT, ni.Core_test_v1_Input_input_array_string_foo123 + "[0]", "hello", expectError{}},

		// connect an index port with a string input
		{[]string{"a", "b", "c"}, ni.Core_test_v1_Output_output_array_string_foo123, ni.Core_test_v1_Output_output_array_string_foo123 + "[0]", ni.Core_test_v1_Input_input_string_foo123, "a", expectError{}},

		// invalid setter
		{"this is just a string", ni.Core_test_v1_Output_output_bool_foo123, SAME_AS_OUTPUT, ni.Core_test_v1_Input_input_string_foo123, "", expectError{setValue: true, getValue: false}},

		// invalid getter (both ports are succesfully connected because they are both of type 'string', but then an int32 is requested).
		{"hello", ni.Core_test_v1_Output_output_string_foo123, SAME_AS_OUTPUT, ni.Core_test_v1_Input_input_string_foo123, int32(1), expectError{setValue: false, getValue: true}},

		// invalid getter (out of index in the input)
		{[]string{"abc", "def", "ghi"}, ni.Core_test_v1_Output_output_array_string_foo123, ni.Core_test_v1_Output_output_array_string_foo123 + "[0]", ni.Core_test_v1_Input_input_array_string_foo123 + "[42]", "abc", expectError{setValue: false, getValue: true}},

		// invalid loader, ports don't exist
		{[]string{"a", "b", "c"}, "inputdoesntexist", "inputdoesntexist", "outputdoesntexist", "", expectError{loading: true}},
		{[]string{"a", "b", "c"}, ni.Core_test_v1_Output_output_array_string_foo123, ni.Core_test_v1_Output_output_array_string_foo123 + "[foo]", ni.Core_test_v1_Input_input_array_string_foo123, "", expectError{loading: true}},
	}

	var err error

	for i, tt := range tests {

		ec := core.NewExecutionState(
			context.Background(),
			nil,
			"test-graph",
			false,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
		)

		setOnPort := tt.setOnPort
		if setOnPort == SAME_AS_OUTPUT {
			setOnPort = tt.setOnOutput
		}

		_, sourceNodeOutputs, dstNode, _, loadErr := createTwoNodesAndConnectThem(setOnPort, tt.inputId)
		if loadErr != nil {
			if tt.expectError.loading {
				leafError, ok := loadErr.(*core.LeafError)
				if !ok {
					t.Errorf("expected LeafError, got %T", loadErr)
					t.Fatal(loadErr)
					continue
				}
				m, err := regexp.Match(`source node '[\w\s]+' \([\w-]+\) has no output '[\w-]+(\[[\w-]+\])?'.*`, []byte(leafError.ErrorWithCauses()))
				if err == nil && m {
					t.Logf("successfully got expected error: %s", loadErr)
					continue
				}
			}
			err = loadErr
		}

		if err == nil {
			setErr := sourceNodeOutputs.SetOutputValue(ec, tt.setOnOutput, tt.output, core.SetOutputValueOpts{})
			if setErr != nil {

				if tt.expectError.setValue {
					m, err := regexp.Match(`output 'Output [\w-]+' \([\w-]+\): expected [\w]+, but got [\[\]\w]+`, []byte(setErr.Error()))
					if err == nil && m {
						t.Logf("successfully got expected error: %s", setErr)
						continue
					}
				}

				err = setErr
			}
		}

		if err == nil {
			switch expected := tt.expected.(type) {
			case int8:
				err = testCastingDeepEqual(t, ec, dstNode, tt.inputId, expected, tt.expectError.getValue)
			case int32:
				err = testCastingDeepEqual(t, ec, dstNode, tt.inputId, expected, tt.expectError.getValue)
			case int64:
				err = testCastingDeepEqual(t, ec, dstNode, tt.inputId, expected, tt.expectError.getValue)
			case bool:
				err = testCastingDeepEqual(t, ec, dstNode, tt.inputId, expected, tt.expectError.getValue)
			case []bool:
				err = testCastingDeepEqual(t, ec, dstNode, tt.inputId, expected, tt.expectError.getValue)
			case []int16:
				err = testCastingDeepEqual(t, ec, dstNode, tt.inputId, expected, tt.expectError.getValue)
			case []int32:
				err = testCastingDeepEqual(t, ec, dstNode, tt.inputId, expected, tt.expectError.getValue)
			case []int64:
				err = testCastingDeepEqual(t, ec, dstNode, tt.inputId, expected, tt.expectError.getValue)
			case string:
				err = testCastingDeepEqual(t, ec, dstNode, tt.inputId, expected, tt.expectError.getValue)
			case []string:
				err = testCastingDeepEqual(t, ec, dstNode, tt.inputId, expected, tt.expectError.getValue)
			case []float64:
				err = testCastingDeepEqual(t, ec, dstNode, tt.inputId, expected, tt.expectError.getValue)
			case io.Reader, core.DataStreamFactory:

				r, ok := tt.expected.(io.Reader)
				if ok {
					var err error
					r, err = core.InputValueById[io.Reader](ec, dstNode.(*TestNode), tt.inputId)
					if err != nil {
						break
					}
				} else {
					df, err := core.InputValueById[core.DataStreamFactory](ec, dstNode.(*TestNode), tt.inputId)
					if err != nil {
						break
					}
					r = df.Reader
				}

				if r == nil {
					t.Fatal("expected io.Reader, got nil")
				}

				buf := make([]byte, 5)
				n, err := r.Read(buf)

				utils.SafeCloseReaderAndIgnoreError(r)
				if err != nil {
					break
				}

				if n != 5 {
					err = fmt.Errorf("expected to read 5 bytes, read %d", n)
					break
				}

				if string(buf) != "hello" {
					err = fmt.Errorf("expected 'hello', got '%s'", string(buf))
					break
				}
			case DummyStorageProvider:
				dp, err := core.InputValueById[DummyStorageProvider](ec, dstNode.(*TestNode), tt.inputId)
				if err != nil {
					break
				}

				list := StorageList{
					Objects: []string{"object1", "object2"},
					Dirs:    []string{"dir1", "dir2"},
				}

				l, err := dp.ListObjects(".")
				if err != nil {
					break
				}

				if !reflect.DeepEqual(l, list) {
					err = fmt.Errorf("expected '%v', got '%v'", l, list)
					break
				}
			default:
				t.Fatal("unsupported type")
			}
		}

		if err != nil {
			t.Logf("Test failed at index: %d", i)
			t.Fatal(err)
		}
	}
}

func testCastingDeepEqual[T any](t *testing.T, ec *core.ExecutionState, dstNode core.NodeBaseInterface, inputId core.InputId, expected T, getExpectErr bool) error {
	value, getErr := core.InputValueById[T](ec, dstNode.(*TestNode), inputId)
	if getErr != nil {
		if getExpectErr && strings.HasPrefix(getErr.Error(), "unable to convert") {
			t.Logf("successfully got expected error: %s", getErr)
			return nil
		}

		return getErr
	}

	if !reflect.DeepEqual(value, expected) {
		return fmt.Errorf("expected '%v', got '%v'", expected, value)
	}

	t.Logf("successfully got '%v'", value)
	return nil
}

func createTwoNodesAndConnectThem(outputId core.OutputId, inputId core.InputId) (srcNode core.NodeBaseInterface, srcOutputs core.HasOutputsInterface, dstNode core.NodeBaseInterface, dstInputs core.HasInputsInterface, err error) {

	var (
		dynamicOutput string
		dynamicInput  string
	)

	if strings.Contains(string(inputId), "[") {
		dynamicInput = fmt.Sprintf(`inputs:
      %s: null`, inputId)
	}

	if strings.Contains(string(outputId), "[") {
		dynamicOutput = fmt.Sprintf(`outputs:
      %s: null`, outputId)
	}

	graph := fmt.Sprintf(`editor:
  version:
    created: v1.11.0
entry: start
type: generic
nodes:
  - id: start
    type: core/start@v1
    position:
      x: -690
      y: -400
  - id: source-node
    type: core/test@v1
    position:
      x: -410
      y: -320
    %s
  - id: target-node
    type: core/test@v1
    position:
      x: 170
      y: -300
    %s
connections:
  - src:
      node: source-node
      port: %s
    dst:
      node: target-node
      port: %s
executions: []
`, dynamicOutput, dynamicInput, outputId, inputId)

	var graphYaml map[string]any
	err = yaml.Unmarshal([]byte(graph), &graphYaml)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	ag, errs := core.LoadGraph(graphYaml, nil, "", false)
	if errs != nil {
		return nil, nil, nil, nil, errs[0]
	}

	srcNode, ok := ag.FindNode("source-node")
	if !ok {
		return nil, nil, nil, nil, errors.New("source-node not found")
	}

	dstNode, ok = ag.FindNode("target-node")
	if !ok {
		return nil, nil, nil, nil, errors.New("target-node not found")
	}

	testOutputs, ok := srcNode.(core.HasOutputsInterface)
	if !ok {
		return nil, nil, nil, nil, errors.New("node does not implement HasOutputsInterface")
	}

	test2Inputs, ok := dstNode.(core.HasInputsInterface)
	if !ok {
		return nil, nil, nil, nil, errors.New("node does not implement HasInputsInterface")
	}

	return srcNode, testOutputs, dstNode, test2Inputs, nil
}

type DummyStorageProvider struct {
}

func (n DummyStorageProvider) GetName() string {
	return "dummy-provider"
}

func (n DummyStorageProvider) ListObjects(dir string) (StorageList, error) {
	return StorageList{
		Objects: []string{"object1", "object2"},
		Dirs:    []string{"dir1", "dir2"},
	}, nil
}

func (n DummyStorageProvider) UploadObject(name string, data io.Reader) error {
	return nil
}

func (n DummyStorageProvider) CanClone(src core.StorageProvider) bool {
	return false
}

func (n DummyStorageProvider) CloneObject(dstName string, src core.StorageProvider, srcName string) error {
	return nil
}

func (n DummyStorageProvider) DownloadObject(name string) (io.Reader, error) {
	return strings.NewReader("hello"), nil
}
