package cmd

import (
	"fmt"
	"os"
	"os/user"
	"strings"

	"github.com/actionforge/actrun-cli/core"
	u "github.com/actionforge/actrun-cli/utils"
	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v4"
)

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

	_, errs := core.LoadGraph(graphYaml, nil, "", true)

	if len(errs) > 0 {
		fmt.Printf("\n❌ Validation failed with %d error(s):\n", len(errs))

		for i, e := range errs {
			if leafErr, ok := e.(*core.LeafError); ok {
				fmt.Printf("\n--- Error %d ---\n", i+1)
				fmt.Printf("%v\n", leafErr)
			} else {
				fmt.Printf("\n%d. %v\n", i+1, e)
			}
		}
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

func init() {
	cmdRoot.AddCommand(cmdValidate)
}
