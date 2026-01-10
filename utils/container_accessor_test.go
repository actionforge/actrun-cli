package utils

import (
	"reflect"
	"testing"
)

func TestGetValueByPath(t *testing.T) {
	tests := []struct {
		name      string
		data      any
		path      string
		wantValue any
		wantErr   bool
	}{
		{
			name: "simple key",
			data: map[string]any{
				"key": "value",
			},
			path:      "key",
			wantValue: "value",
			wantErr:   false,
		},
		{
			name: "nested key",
			data: map[string]any{
				"a": map[string]any{
					"b": "value",
				},
			},
			path:      "a.b",
			wantValue: "value",
			wantErr:   false,
		},
		{
			name: "array index",
			data: map[string]any{
				"a": []any{
					"value",
				},
			},
			path:      "a[0]",
			wantValue: "value",
			wantErr:   false,
		},
		{
			name: "nested array",
			data: map[string]any{
				"a": map[string]any{
					"b": []any{
						map[string]any{
							"c": "value",
						},
					},
				},
			},
			path:      "a.b[0].c",
			wantValue: "value",
			wantErr:   false,
		},
		{
			name: "invalid key",
			data: map[string]any{
				"key": "value",
			},
			path:      "invalid",
			wantValue: nil,
			wantErr:   true,
		},
		{
			name: "index out of range",
			data: map[string]any{
				"a": []any{
					"value",
				},
			},
			path:      "a[1]",
			wantValue: nil,
			wantErr:   true,
		},
		{
			name: "empty path",
			data: map[string]any{
				"key": "value",
			},
			path:      "",
			wantValue: nil,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotValue, err := GetPropertyByPath(tt.data, tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("getValueByPath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(gotValue, tt.wantValue) {
				t.Errorf("getValueByPath() gotValue = %v, want %v", gotValue, tt.wantValue)
			}
		})
	}
}

func TestExtractNumbers(t *testing.T) {
	tests := []struct {
		input           string
		expectedKey     string
		expectedNumbers []int
		expectErr       bool
	}{
		{"Foo[1][2][3]", "Foo", []int{1, 2, 3}, false},
		{"Bar[0][9][8]", "Bar", []int{0, 9, 8}, false},
		{"Baz[123]", "Baz", []int{123}, false},
		{"NoNumbers", "NoNumbers", nil, false},
		{"Empty[]", "", nil, true},
		{"Invalid[Number][12a]", "", nil, true},
		{"OnlyNumbers[42]", "OnlyNumbers", []int{42}, false},
		{"Mixed[12]Content[34]", "Mixed", []int{12, 34}, false},
		{"Complex[1][23][456]", "Complex", []int{1, 23, 456}, false},
		{"Nested[12][34][56][78][90]", "Nested", []int{12, 34, 56, 78, 90}, false},
		{"TrailingNumbers[789]", "TrailingNumbers", []int{789}, false},
		{"", "", nil, false},
		{"[123]", "", []int{123}, false},
		{"KeyWithNoBrackets", "KeyWithNoBrackets", nil, false},
	}

	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			key, numbers, err := extractNumbers(test.input)

			if (err != nil) != test.expectErr {
				t.Errorf("expected error: %v, got: %v", test.expectErr, err)
			}

			if key != test.expectedKey {
				t.Errorf("expected key: %v, got: %v", test.expectedKey, key)
			}

			if !reflect.DeepEqual(numbers, test.expectedNumbers) {
				t.Errorf("expected numbers: %v, got: %v", test.expectedNumbers, numbers)
			}
		})
	}
}

func TestSetValueByPath(t *testing.T) {
	tests := []struct {
		name      string
		data      any
		path      string
		value     any
		expectErr bool
		expected  any
	}{
		{
			name:      "empty path",
			data:      map[string]any{},
			path:      "",
			value:     "test",
			expectErr: true,
		},
		{
			name: "simple map",
			data: map[string]any{
				"a": "initial",
			},
			path:      "a",
			value:     "test",
			expectErr: false,
			expected: map[string]any{
				"a": "test",
			},
		},
		{
			name: "nested map",
			data: map[string]any{
				"a": map[string]any{
					"b": "initial",
				},
			},
			path:      "a.b",
			value:     "test",
			expectErr: false,
			expected: map[string]any{
				"a": map[string]any{
					"b": "test",
				},
			},
		},
		{
			name: "nested map with array",
			data: map[string]any{
				"a": map[string]any{
					"b": []any{"initial"},
				},
			},
			path:      "a.b[0]",
			value:     "test",
			expectErr: false,
			expected: map[string]any{
				"a": map[string]any{
					"b": []any{"test"},
				},
			},
		},
		{
			name: "invalid path",
			data: map[string]any{
				"a": "initial",
			},
			path:      "a[0]",
			value:     "test",
			expectErr: false,
			expected: map[string]any{
				"a": []any{"test"},
			},
		},
		{
			name:      "create nested map",
			data:      map[string]any{},
			path:      "a.b.c",
			value:     "test",
			expectErr: false,
			expected: map[string]any{
				"a": map[string]any{
					"b": map[string]any{
						"c": "test",
					},
				},
			},
		},
		{
			name:      "create nested map with array",
			data:      map[string]any{},
			path:      "a.b[0].c",
			value:     "test",
			expectErr: false,
			expected: map[string]any{
				"a": map[string]any{
					"b": []any{
						map[string]any{
							"c": "test",
						},
					},
				},
			},
		},
		{
			name: "overwrite existing map value",
			data: map[string]any{
				"a": map[string]any{
					"b": "initial",
				},
			},
			path:      "a.b",
			value:     "test",
			expectErr: false,
			expected: map[string]any{
				"a": map[string]any{
					"b": "test",
				},
			},
		},
		{
			name: "overwrite existing array value",
			data: map[string]any{
				"a": []any{"initial"},
			},
			path:      "a[0]",
			value:     "test",
			expectErr: false,
			expected: map[string]any{
				"a": []any{"test"},
			},
		},
		{
			name: "set value in multi-dimensional array",
			data: map[string]any{
				"a": [][]any{
					{"initial"},
				},
			},
			path:      "a[0][0]",
			value:     "test",
			expectErr: false,
			expected: map[string]any{
				"a": [][]any{
					{"test"},
				},
			},
		}, {
			name: "replace map with slice using index",
			data: map[string]any{
				"a": map[string]any{
					"b": "initial",
				},
			},
			path:      "a[1]",
			value:     "test",
			expectErr: false,
			expected: map[string]any{
				"a": []any{nil, "test"},
			},
		}, {
			name: "set value in multi-dimensional array",
			data: map[string]any{
				"a": [][]any{
					{"initial"},
				},
			},
			path:      "a[0][0]",
			value:     "test",
			expectErr: false,
			expected: map[string]any{
				"a": [][]any{
					{"test"},
				},
			},
		},
		{
			name: "different type in path",
			data: map[string]any{
				"a": "some string",
			},
			path:  "a.b",
			value: "foo",
			expected: map[string]any{
				"a": map[string]any{
					"b": "foo",
				},
			},
			expectErr: false,
		},
		{
			name: "nonexistent array index",
			data: map[string]any{
				"a": []any{},
			},
			path:      "a[1]",
			value:     "test",
			expectErr: false,
			expected: map[string]any{
				"a": []any{nil, "test"},
			},
		},
		{
			name: "append to array",
			data: map[string]any{
				"a": []any{"first"},
			},
			path:      "a[1]",
			value:     "second",
			expectErr: false,
			expected: map[string]any{
				"a": []any{"first", "second"},
			},
		},
		{
			name:      "deeply nested structure",
			data:      map[string]any{},
			path:      "a.b[0].c[1].d",
			value:     "test",
			expectErr: false,
			expected: map[string]any{
				"a": map[string]any{
					"b": []any{
						map[string]any{
							"c": []any{
								nil,
								map[string]any{
									"d": "test",
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := SetPropertyByPath(tt.data, tt.path, tt.value)
			if (err != nil) != tt.expectErr {
				t.Errorf("setValueByPath() error = %v, expectErr %v", err, tt.expectErr)
				return
			}
			if !tt.expectErr && !reflect.DeepEqual(data, tt.expected) {
				t.Errorf("setValueByPath() = %v, expected %v", tt.data, tt.expected)
			}
		})
	}
}
