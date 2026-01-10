//go:build dev

package cmd

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"text/template"

	// initialize all nodes
	"github.com/actionforge/actrun-cli/core"
	_ "github.com/actionforge/actrun-cli/nodes"

	"github.com/spf13/cobra"
	"golang.org/x/exp/maps"
)

const templateMarkdown = `---
title: {{ .Name }}
---

{{ printf "{{graph_editor_node( '%s', docs=true, background=false, header=false, permission='readonly', console=false, toolbar=false, translate=false, fade=false, classes='%s', autofit=true )}}" .Id .TailwindHeight }}

{{ if .ShortDesc}}{{.ShortDesc}}{{end}}
{{if .LongDesc}}
{{.LongDesc}}
{{- end}}
{{if gt (len .Inputs) 0 }}
## Inputs
| Port | Description |
| ---- | ----------- |
{{- range $key, $value := .Inputs }}
| <div class="flex flex-row items-center gap-x-2" title="{{$key}}"> {{"{{"}} port('{{if .Exec}}exec{{else}}{{.Type}}{{- end}}') {{"}}"}} {{if .Name}}**{{.Name}}**{{end}}</div> | {{.Desc}} |
{{- end}}
{{- end}}

{{if gt (len .Outputs) 0 }}
## Outputs
| Port | Description |
| ---- | ----------- |
{{- range $key, $value := .Outputs }}
| <div class="flex flex-row items-center gap-x-2" title="{{$key}}"> {{"{{"}} port('{{if .Exec}}exec{{else}}{{.Type}}{{- end}}') {{"}}"}} {{if .Name}}**{{.Name}}**{{end}}</div> | {{.Desc}} |
{{- end}}
{{- end}}

{{ if .Addendum }}
## Addendum
{{ .Addendum }}
{{- end}}

ID: ` + "`" + `{{.Id}}` + "`" + `
`

var cmdDevUpdateDocs = &cobra.Command{
	Use:   "docs",
	Short: "Update the docs for nodes",
	Run: func(cmd *cobra.Command, args []string) {
		err := devUpdateDocs()
		if err != nil {
			log.Fatal(err)
		}
	},
}

func EmptyDirectory(path string) error {
	return filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			err := os.Remove(path)
			if err != nil {
				return err
			}
		}

		return nil
	})
}

func generatePagesFiles(nodesDir string) error {

	// Generate .pages file
	pagesPath := filepath.Join(nodesDir, ".pages")
	f, err := os.Create(pagesPath)
	if err != nil {
		return core.CreateErr(nil, nil, "error creating .pages file")
	}
	defer f.Close()

	_, err = f.WriteString("title: Nodes\nnav:\n")
	if err != nil {
		return core.CreateErr(nil, nil, "error writing to .pages file")
	}

	// Collect and sort node IDs for deterministic nav order
	nodeIds := make([]string, 0)
	for nodeId, nodeDef := range core.GetRegistries() {
		if nodeDef.ShowInDocs != nil && !*nodeDef.ShowInDocs {
			continue
		}
		nodeIds = append(nodeIds, nodeId)
	}
	sort.Strings(nodeIds)

	for _, nodeId := range nodeIds {
		nodeDef := core.GetRegistries()[nodeId]
		name := nodeDef.Name
		if name == "" {
			name = nodeId
		}
		// Convert nodeId to path, e.g. core/process-exit@v1 -> nodes/core/process-exit/v1
		path := "nodes/" + strings.ReplaceAll(nodeId, "@", "/")
		// Write nav entry
		_, err := fmt.Fprintf(f, "  - %s: %s\n", name, path)
		if err != nil {
			return core.CreateErr(nil, nil, "error writing nav entry to .pages file")
		}
	}
	return nil
}

func devUpdateDocs() error {
	exePath, err := os.Executable()
	if err != nil {
		return core.CreateErr(nil, nil, "error getting executable path")
	}

	nodesDir := filepath.Join(filepath.Dir(exePath), "..", "docs", "docs", "nodes")
	err = os.MkdirAll(nodesDir, 0755)
	if err != nil {
		return core.CreateErr(nil, nil, "error creating nodes directory")
	}

	err = EmptyDirectory(nodesDir)
	if err != nil {
		return err
	}

	for nodeId, nodeDef := range core.GetRegistries() {

		if nodeDef.ShowInDocs != nil && !*nodeDef.ShowInDocs {
			continue
		}

		mdContent := generateMarkdown(nodeId, nodeDef)

		nodeId = strings.Replace(nodeId, "@", "/", 1)

		fname := fmt.Sprintf("%s.md", strings.ReplaceAll(nodeId, "@", "-"))
		fpath := filepath.Join(nodesDir, fname)
		fmt.Printf("Write %s\n", fpath)
		err := os.MkdirAll(filepath.Dir(fpath), 0755)
		if err != nil {
			return core.CreateErr(nil, nil, "error creating parent directory for markdown file")
		}
		err = os.WriteFile(fpath, []byte(mdContent), 0644)
		if err != nil {
			return core.CreateErr(nil, nil, "error writing markdown file")
		}
	}

	err = generatePagesFiles(nodesDir)
	if err != nil {
		return err
	}

	return nil
}

// Generate a Markdown description of a node
func generateMarkdown(nodeId string, nodeDef core.NodeTypeDefinitionFull) string {
	var sb strings.Builder

	// TailwindCSS Heights
	// https://tailwindcss.com/docs/height
	tailwindSizes := []int{12, 16, 20, 24, 28, 32, 36, 40, 44, 48, 52, 56, 60, 64, 72, 80, 96}
	rowSize := 8

	inputCnt := len(nodeDef.Inputs)
	for _, input := range nodeDef.Inputs {
		inputCnt += input.ArrayInitialCount
	}

	outputCnt := len(nodeDef.Outputs)
	for _, output := range nodeDef.Outputs {
		outputCnt += output.ArrayInitialCount
	}

	tailwindHeight := roundUpToNextInRange(tailwindSizes, int(inputCnt+outputCnt)*rowSize)

	inputKeys := maps.Keys(nodeDef.Inputs)
	sort.Slice(inputKeys, func(i, j int) bool {
		return nodeDef.Inputs[inputKeys[i]].Index < nodeDef.Inputs[inputKeys[j]].Index
	})

	outputKeys := maps.Keys(nodeDef.Outputs)
	sort.Slice(outputKeys, func(i, j int) bool {
		return nodeDef.Outputs[outputKeys[i]].Index < nodeDef.Outputs[outputKeys[j]].Index
	})

	nodeDataMap := structToMap(nodeDef)
	nodeDataMap["TailwindHeight"] = fmt.Sprintf("h-%d", tailwindHeight)
	nodeDataMap["Id"] = nodeId

	inputs := make([]core.InputDefinition, 0)
	for _, inputId := range inputKeys {
		input := nodeDef.Inputs[inputId]

		if input.HideSocket {
			continue
		}
		inputs = append(inputs, input)
	}
	nodeDataMap["Inputs"] = inputs

	outputs := make([]core.OutputDefinition, 0)
	for _, outputId := range outputKeys {
		output := nodeDef.Outputs[outputId]

		outputs = append(outputs, output)
	}
	nodeDataMap["Outputs"] = outputs

	t, err := template.New("node").Parse(templateMarkdown)
	if err != nil {
		panic(err)
	}

	err = t.Execute(&sb, nodeDataMap)
	if err != nil {
		panic(err)
	}

	return sb.String()
}

func structToMap(obj any) map[string]any {
	val := reflect.ValueOf(obj)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	dataMap := make(map[string]any)
	for i := 0; i < val.Type().NumField(); i++ {
		field := val.Type().Field(i)
		value := val.Field(i)

		if field.Anonymous && value.Kind() == reflect.Struct {
			embeddedDataMap := structToMap(value.Interface())
			for k, v := range embeddedDataMap {
				dataMap[k] = v
			}
		} else if value.Kind() == reflect.Struct {
			dataMap[field.Name] = structToMap(value.Interface())
		} else {
			dataMap[field.Name] = value.Interface()
		}
	}

	return dataMap
}

func roundUpToNextInRange(slice []int, target int) int {
	sort.Ints(slice)
	if target <= slice[0] {
		return slice[0]
	}
	if target > slice[len(slice)-1] {
		return slice[len(slice)-1]
	}
	idx := sort.Search(len(slice), func(i int) bool {
		return slice[i] >= target
	})
	if idx < len(slice) {
		return slice[idx]
	}
	return -1
}

func init() {
	cmdDevUpdate.AddCommand(cmdDevUpdateDocs)
}
