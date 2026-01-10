package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEvaluate_GitHubParity(t *testing.T) {
	ctx := ExecutionState{
		GhNeeds: map[string]any{
			"setup": map[string]any{
				"outputs": map[string]any{
					"string-true":   "true",
					"string-false":  "false",
					"string-one":    "1",
					"string-zero":   "0",
					"string-number": "100",
					"result":        "success",
				},
				"result": "success",
			},
			"failure-job": map[string]any{
				"result": "success",
			},
		},
		Env: map[string]string{
			"SIMPLE_VAR": "simple_value",
			"CASE_TEST":  "UPPER_VALUE",
			"case_test":  "lower_value",
			"Mixed_Case": "Mixed_Value",
		},
	}

	evaluator := NewEvaluator(&ctx)

	tests := []struct {
		name     string
		input    string
		expected any
	}{
		// Equality Tests (==)
		{name: "String vs Number: '1' == 1", input: "${{ '1' == 1 }}", expected: true},
		{name: "String vs Number: '0' == 0", input: "${{ '0' == 0 }}", expected: true},
		{name: "String vs Number: '100' == 100", input: "${{ '100' == 100 }}", expected: true},
		{name: "String vs Number: '3.14' == 3.14", input: "${{ '3.14' == 3.14 }}", expected: true},
		{name: "String vs Number: 'abc' == 123", input: "${{ 'abc' == 123 }}", expected: false},

		{name: "String vs Bool: 'true' == true", input: "${{ 'true' == true }}", expected: false},
		{name: "String vs Bool: 'false' == false", input: "${{ 'false' == false }}", expected: false},
		{name: "String vs Bool: 'True' == true", input: "${{ 'True' == true }}", expected: false},
		{name: "String vs Bool: '1' == true", input: "${{ '1' == true }}", expected: true},
		{name: "String vs Bool: '0' == false", input: "${{ '0' == false }}", expected: true},

		{name: "Number vs Bool: 1 == true", input: "${{ 1 == true }}", expected: true},
		{name: "Number vs Bool: 0 == false", input: "${{ 0 == false }}", expected: true},
		{name: "Number vs Bool: 2 == true", input: "${{ 2 == true }}", expected: false},
		{name: "Number vs Bool: -1 == true", input: "${{ -1 == true }}", expected: false},

		{name: "Tricky: 'true' == 1", input: "${{ 'true' == 1 }}", expected: false},
		{name: "Tricky: 'false' == 0", input: "${{ 'false' == 0 }}", expected: false},
		{name: "Tricky: 1 == 'true'", input: "${{ 1 == 'true' }}", expected: false},
		{name: "Tricky: '' == 0", input: "${{ '' == 0 }}", expected: true},
		{name: "Tricky: '' == false", input: "${{ '' == false }}", expected: true},
		{name: "Tricky: '0.0' == 0", input: "${{ '0.0' == 0 }}", expected: true},
		{name: "Tricky: '  1  ' == 1", input: "${{ '  1  ' == 1 }}", expected: true},

		// Inequality & Relational (!=, <, >)
		{name: "Inequality: '1' != 1", input: "${{ '1' != 1 }}", expected: false},
		{name: "Inequality: 'true' != 1", input: "${{ 'true' != 1 }}", expected: true},
		{name: "Inequality: 'true' != true", input: "${{ 'true' != true }}", expected: true},

		{name: "Relational: '100' > 50", input: "${{ '100' > 50 }}", expected: true},
		{name: "Relational: '10' < 100", input: "${{ '10' < 100 }}", expected: true},
		{name: "Relational: '5.5' >= 5.0", input: "${{ '5.5' >= 5.0 }}", expected: true},

		{name: "Relational String: 'abc' < 'xyz'", input: "${{ 'abc' < 'xyz' }}", expected: true},
		{name: "Relational String: 'ABC' < 'xyz'", input: "${{ 'ABC' < 'xyz' }}", expected: true},

		{name: "Invalid Compare: 'abc' > 100", input: "${{ 'abc' > 100 }}", expected: false},
		{name: "Invalid Compare: true > 1", input: "${{ true > 1 }}", expected: false},

		// Truthiness & Logical Operators
		{name: "Truthiness: !('true')", input: "${{ !('true') }}", expected: false},
		{name: "Truthiness: !('false')", input: "${{ !('false') }}", expected: false}, // "false" string is truthy
		{name: "Truthiness: !('0')", input: "${{ !('0') }}", expected: false},         // "0" string is truthy
		{name: "Truthiness: !('1')", input: "${{ !('1') }}", expected: false},
		{name: "Truthiness: !('')", input: "${{ !('') }}", expected: true},
		{name: "Truthiness: !('abc')", input: "${{ !('abc') }}", expected: false},

		{name: "Logical OR: '0' || 'default'", input: "${{ '0' || 'default' }}", expected: "0"},
		{name: "Logical OR: '' || 'default'", input: "${{ '' || 'default' }}", expected: "default"},
		{name: "Logical OR: 'false' || 'default'", input: "${{ 'false' || 'default' }}", expected: "false"},

		{name: "Logical AND: '1' && 'value'", input: "${{ '1' && 'value' }}", expected: "value"},
		{name: "Logical AND: '0' && 'value'", input: "${{ '0' && 'value' }}", expected: "value"},
		{name: "Logical AND: 'true' && 'false'", input: "${{ 'true' && 'false' }}", expected: "false"},

		// Functions
		// Note: fromJSON numbers usually become float64 in Go unmarshaling.
		// here we use generic equality checks, verify strict types if necessary.
		{name: "fromJSON Array Access", input: "${{ fromJSON('[1,2,3]')[1] }}", expected: 2.0 /* NOT 2 */},
		{name: "toJSON", input: "${{ toJSON(fromJSON('[\"a\",\"b\"]')) }}", expected: "[\n  \"a\",\n  \"b\"\n]"},

		{name: "contains string", input: "${{ contains('hello', 'ell') }}", expected: true},
		{name: "contains array", input: "${{ contains(fromJSON('[1,2,3]'), 2) }}", expected: true},
		{name: "startsWith", input: "${{ startsWith('hello', 'hel') }}", expected: true},
		{name: "endsWith", input: "${{ endsWith('hello', 'lo') }}", expected: true},
		{name: "format", input: "${{ format('Hello {0} {1}', 'World', '!') }}", expected: "Hello World !"},
		{name: "join", input: "${{ join(fromJSON('[\"a\",\"b\",\"c\"]'), ', ') }}", expected: "a, b, c"},

		// Context Access & Status Checks
		{name: "Status: success()", input: "${{ success() }}", expected: true},
		{name: "Status: failure()", input: "${{ failure() }}", expected: false},
		{name: "Status: cancelled()", input: "${{ cancelled() }}", expected: false},

		{name: "Context: needs access", input: "${{ needs.setup.outputs.string-true }}", expected: "true"},
		{name: "Context: needs == 'true'", input: "${{ needs.setup.outputs.string-true == 'true' }}", expected: true},
		{name: "Context: needs == true", input: "${{ needs.setup.outputs.string-true == true }}", expected: false},
		{name: "Context: needs one == 1", input: "${{ needs.setup.outputs.string-one == 1 }}", expected: true},

		// Object Filtering (Wildcard)
		{name: "Wildcard: contains success", input: "${{ contains(needs.*.result, 'success') }}", expected: true},
		{name: "Wildcard: contains failure", input: "${{ contains(needs.*.result, 'failure') }}", expected: false},

		// Edge Cases & Null Handling
		{name: "Null: null == null", input: "${{ null == null }}", expected: true},
		{name: "Null: null == ''", input: "${{ null == '' }}", expected: true},
		{name: "Null: null == 0", input: "${{ null == 0 }}", expected: true},
		{name: "Null: null == false", input: "${{ null == false }}", expected: true},
		{name: "Null: Missing obj", input: "${{ github.event.non_existent == null }}", expected: true},

		// Environment Variables & Case Sensitivity
		{name: "Env: Simple Access", input: "${{ env.SIMPLE_VAR }}", expected: "simple_value"},
		{name: "Env: Case Sensitivity (Upper)", input: "${{ env.CASE_TEST }}", expected: "UPPER_VALUE"},
		{name: "Env: Case Sensitivity (Lower)", input: "${{ env.case_test }}", expected: "lower_value"},
		{name: "Env: Mixed Case Preservation", input: "${{ env.Mixed_Case }}", expected: "Mixed_Value"},

		// Verify they are not equal
		{name: "Env: Check Casing Inequality", input: "${{ env.CASE_TEST != env.case_test }}", expected: true},

		// Check that wrong casing results in null (missing)
		{name: "Env: Incorrect Casing is Null", input: "${{ env.mixed_case == null }}", expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := evaluator.Evaluate(tt.input)
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}
