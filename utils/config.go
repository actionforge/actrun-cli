package utils

import (
	"fmt"
	"os"
	"strings"

	"go.yaml.in/yaml/v4"
)

type Config struct {
	values map[string]any
	Path   string

	// This is the map of secrets that are available during the execution
	// of the action graph. The values contain the context name and
	// the secret value. Example: 'secrets.input1'
	Secrets map[string]string

	// this is the map of all 'inputs.xyz' variables
	Inputs map[string]any

	Env map[string]string
}

func (c *Config) LoadFile(filePath string) error {
	c.Path = filePath

	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	var parsedData map[string]any
	err = yaml.Unmarshal(data, &parsedData)
	if err != nil {
		return err
	}

	linearData := flatten(parsedData, "")
	for k, v := range linearData {
		// if the high level key is missing a dot and the value contains
		// an equal sign, the user might have accidentally used this syntax:
		// env:
		//   MY_VAR=foo
		// instead of:
		// env:
		//   MY_VAR: foo
		vs := fmt.Sprintf("%v", v)
		if !strings.Contains(k, ".") && strings.Contains(vs, "=") {
			return fmt.Errorf("incorrect syntax, use ':' instead of '=' in key '%s'", k)
		}
		c.values[k] = v
	}

	return nil
}

func (c *Config) Get(key string) string {
	v := c.values[key]
	if v == nil {
		return ""
	} else {
		return fmt.Sprintf("%v", v)
	}
}

func (c *Config) GetAll(keyPrefix string) map[string]any {
	values := map[string]any{}
	for k, v := range c.values {
		if k == keyPrefix || strings.HasPrefix(k, keyPrefix+".") {
			k = strings.TrimPrefix(k, keyPrefix+".")
			values[k] = v
		}
	}
	return values
}

func flatten(data map[string]any, prefix string) map[string]any {
	flatMap := make(map[string]any)
	for k, v := range data {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		switch v := v.(type) {
		case map[any]any:
			// Convert map[any]any to map[string]any
			stringMap := make(map[string]any)
			for key, value := range v {
				stringMap[fmt.Sprintf("%v", key)] = value
			}
			for subKey, subValue := range flatten(stringMap, key) {
				flatMap[subKey] = subValue
			}
		case map[string]any:
			for subKey, subValue := range flatten(v, key) {
				flatMap[subKey] = subValue
			}
		default:
			flatMap[key] = v
		}
	}
	return flatMap
}
