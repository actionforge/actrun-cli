package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"strings"

	"github.com/actionforge/actrun-cli/core"
	u "github.com/actionforge/actrun-cli/utils"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v4"
)

// ActfileSchema holds the embedded JSON schema bytes, set from main.
var ActfileSchema []byte

var cmdValidate = &cobra.Command{
	Use:   "validate [graph-file]",
	Short: "Validate a graph file.",
	Long:  `Validates the structure, types, connections, and required inputs of an ActionForge graph file without executing it.`,
	Args:  cobra.ArbitraryArgs,
	Run: func(cmd *cobra.Command, args []string) {

		graphFile, _ := u.ResolveCliParam("graph_file", u.ResolveCliParamOpts{
			Flag:      false, // only provided via env, config, or positional arg
			Env:       true,
			Optional:  true,
			ActPrefix: true,
		})
		if graphFile == "" {
			if len(args) > 0 {
				graphFile = args[0]
			} // if no args, let validateGraph handle the error
		}

		err := validateGraph(graphFile)
		if err != nil {
			os.Exit(1)
		}
	},
}

func validateSchema(data any) error {
	if len(ActfileSchema) == 0 {
		return fmt.Errorf("actfile schema not loaded")
	}

	var schemaObj any
	if err := json.Unmarshal(ActfileSchema, &schemaObj); err != nil {
		return fmt.Errorf("failed to parse schema JSON: %w", err)
	}

	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("actfile-schema.json", schemaObj); err != nil {
		return fmt.Errorf("failed to add schema resource: %w", err)
	}

	schema, err := compiler.Compile("actfile-schema.json")
	if err != nil {
		return fmt.Errorf("failed to compile schema: %w", err)
	}

	return schema.Validate(convertToJSONCompatible(data))
}

// convertToJSONCompatible recursively converts YAML-unmarshalled data into
// types that the JSON schema validator accepts.
func convertToJSONCompatible(v any) any {
	switch val := v.(type) {
	case map[string]any:
		result := make(map[string]any, len(val))
		for k, v := range val {
			result[k] = convertToJSONCompatible(v)
		}
		return result
	case []any:
		result := make([]any, len(val))
		for i, v := range val {
			result[i] = convertToJSONCompatible(v)
		}
		return result
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case float32:
		return float64(val)
	default:
		return val
	}
}

func validateGraph(filePath string) error {
	fmt.Printf("Validating '%s'...\n", filePath)

	content, err := os.ReadFile(expandPath(filePath))
	if err != nil {
		fmt.Printf("Error reading file: %v\n", err)
		return err
	}

	var graphYaml map[string]any
	err = yaml.Unmarshal(content, &graphYaml)
	if err != nil {
		fmt.Printf("Error parsing YAML: %v\n", err)
		return err
	}

	hasErrors := false

	if err := validateSchema(graphYaml); err != nil {
		fmt.Printf("\n❌ Graph schema validation failed:\n%v\n", err)
		hasErrors = true
	}

	_, errs := core.LoadGraph(graphYaml, nil, "", true, core.RunOpts{})

	if len(errs) > 0 {
		fmt.Printf("\n❌ Graph validation failed with %d error(s):\n", len(errs))

		for i, e := range errs {
			if leafErr, ok := e.(*core.LeafError); ok {
				fmt.Printf("\n--- Error %d ---\n", i+1)
				fmt.Printf("%v\n", leafErr)
			} else {
				fmt.Printf("\n%d. %v\n", i+1, e)
			}
		}
		hasErrors = true
	}

	if hasErrors {
		return fmt.Errorf("validation failed")
	}

	fmt.Println("\n✅ Graph is valid.")
	return nil
}

func expandPath(path string) string {
	if strings.HasPrefix(path, "~") {
		usr, err := user.Current()
		if err == nil {
			return strings.Replace(path, "~", usr.HomeDir, 1)
		}
	}
	return os.ExpandEnv(path)
}

var cmdSchema = &cobra.Command{
	Use:   "schema",
	Short: "Print the JSON schema for .act files.",
	Long:  `Prints the JSON schema used to validate ActionForge graph (.act) files.`,
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(string(ActfileSchema))
	},
}

func init() {
	cmdRoot.AddCommand(cmdValidate)
	cmdRoot.AddCommand(cmdSchema)
}
