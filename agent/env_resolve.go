package agent

import (
	"fmt"
	"strings"
)

// resolveEnvMappings resolves env mapping rules against matrix values.
// Supports two mapping types:
// - Template strings: "{{os}}-{{arch}}" → substitutes placeholders
// - Value maps: {"macos": "macos-14", "linux": "ubuntu-24.04"} → looks up matrix value
func resolveEnvMappings(mappings map[string]interface{}, matrixValues map[string]string) (map[string]string, error) {
	result := make(map[string]string, len(mappings))

	for envKey, rule := range mappings {
		resolved, err := resolveMapping(rule, matrixValues)
		if err != nil {
			return nil, fmt.Errorf("env var %s: %w", envKey, err)
		}
		result[envKey] = resolved
	}

	return result, nil
}

func resolveMapping(rule interface{}, matrixValues map[string]string) (string, error) {
	switch v := rule.(type) {
	case string:
		// Template string: replace {{key}} placeholders
		return resolveTemplate(v, matrixValues)
	case map[string]interface{}:
		// Value map: find the dimension key that has a matching matrix value
		// The map is { "dimension_value": "resolved_value", ... }
		// We need to figure out which dimension this map corresponds to
		// by checking which matrix value appears as a key in the map
		for dimValue, resolvedVal := range v {
			// Check if any matrix dimension has this value
			for _, mv := range matrixValues {
				if mv == dimValue {
					if s, ok := resolvedVal.(string); ok {
						return s, nil
					}
					return fmt.Sprintf("%v", resolvedVal), nil
				}
			}
		}
		// If no direct match found, try to find the value via dimension key
		// The map format could also be: { "macos": "macos-14", "linux": "ubuntu-24.04" }
		// where keys are possible values of a matrix dimension
		for _, mv := range matrixValues {
			if resolved, ok := v[mv]; ok {
				if s, ok := resolved.(string); ok {
					return s, nil
				}
				return fmt.Sprintf("%v", resolved), nil
			}
		}
		return "", fmt.Errorf("no matching value in map for matrix values %v", matrixValues)
	default:
		return fmt.Sprintf("%v", rule), nil
	}
}

func resolveTemplate(tmpl string, matrixValues map[string]string) (string, error) {
	result := tmpl
	for key, val := range matrixValues {
		result = strings.ReplaceAll(result, "{{"+key+"}}", val)
	}
	// Check for unresolved placeholders
	if strings.Contains(result, "{{") && strings.Contains(result, "}}") {
		return "", fmt.Errorf("unresolved placeholder in template %q", tmpl)
	}
	return result, nil
}
