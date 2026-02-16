package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/actionforge/actrun-cli/core"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v4"
)

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: text,
			},
		},
	}
}

func errorResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: msg,
			},
		},
		IsError: true,
	}
}

func jsonResult(v any) *mcp.CallToolResult {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return errorResult(fmt.Sprintf("failed to marshal result: %v", err))
	}
	return textResult(string(data))
}

// registerGraphTools registers the non-debug graph tools on the server.
func registerGraphTools(s *server.MCPServer, actfileSchema []byte) {
	s.AddTool(
		mcp.NewTool("validate_graph",
			mcp.WithDescription("Validate a graph YAML string against the JSON schema and structural rules. Returns a list of errors or a success message."),
			mcp.WithString("graph",
				mcp.Description("The full YAML content of the .act graph file"),
				mcp.Required(),
			),
		),
		handleValidateGraph(actfileSchema),
	)

	s.AddTool(
		mcp.NewTool("get_schema",
			mcp.WithDescription("Return the JSON schema for ActionForge .act graph files"),
		),
		handleGetSchema(actfileSchema),
	)

	s.AddTool(
		mcp.NewTool("list_node_types",
			mcp.WithDescription("List all registered node types with id, name, version, category, and short description"),
			mcp.WithString("category",
				mcp.Description("Optional category filter (e.g. 'processing', 'control-flow')"),
			),
		),
		handleListNodeTypes(),
	)

	s.AddTool(
		mcp.NewTool("get_node_type",
			mcp.WithDescription("Get full details for a specific node type including inputs, outputs, and descriptions"),
			mcp.WithString("node_type_id",
				mcp.Description("The node type ID (e.g. 'core/start@v1')"),
				mcp.Required(),
			),
		),
		handleGetNodeType(),
	)
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

func handleValidateGraph(actfileSchema []byte) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		graph, err := req.RequireString("graph")
		if err != nil {
			return errorResult("missing required parameter: graph"), nil
		}

		var graphYaml map[string]any
		if err := yaml.Unmarshal([]byte(graph), &graphYaml); err != nil {
			return jsonResult(map[string]any{
				"valid":  false,
				"errors": []string{fmt.Sprintf("YAML parse error: %v", err)},
			}), nil
		}

		var allErrors []string

		// Schema validation
		if len(actfileSchema) > 0 {
			if err := validateSchema(actfileSchema, graphYaml); err != nil {
				allErrors = append(allErrors, fmt.Sprintf("schema: %v", err))
			}
		}

		// Structural validation
		_, errs := core.LoadGraph(graphYaml, nil, "", true, core.RunOpts{})
		for _, e := range errs {
			allErrors = append(allErrors, e.Error())
		}

		if len(allErrors) > 0 {
			return jsonResult(map[string]any{
				"valid":  false,
				"errors": allErrors,
			}), nil
		}

		return jsonResult(map[string]any{
			"valid": true,
		}), nil
	}
}

func validateSchema(schemaBytes []byte, data any) error {
	var schemaObj any
	if err := json.Unmarshal(schemaBytes, &schemaObj); err != nil {
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

func handleGetSchema(actfileSchema []byte) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if len(actfileSchema) == 0 {
			return errorResult("schema not available"), nil
		}
		return textResult(string(actfileSchema)), nil
	}
}

type nodeTypeSummary struct {
	Id        string `json:"id"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	Category  string `json:"category"`
	ShortDesc string `json:"short_desc"`
}

func handleListNodeTypes() server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		category := req.GetString("category", "")

		registries := core.GetRegistries()
		result := make([]nodeTypeSummary, 0, len(registries))

		for _, def := range registries {
			if category != "" && !strings.EqualFold(def.Category, category) {
				continue
			}
			result = append(result, nodeTypeSummary{
				Id:        def.Id,
				Name:      def.Name,
				Version:   def.Version,
				Category:  def.Category,
				ShortDesc: def.ShortDesc,
			})
		}

		sort.Slice(result, func(i, j int) bool {
			return result[i].Id < result[j].Id
		})

		return jsonResult(result), nil
	}
}

func handleGetNodeType() server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		nodeTypeId, err := req.RequireString("node_type_id")
		if err != nil {
			return errorResult("missing required parameter: node_type_id"), nil
		}

		registries := core.GetRegistries()
		def, ok := registries[nodeTypeId]
		if !ok {
			return errorResult(fmt.Sprintf("node type %q not found", nodeTypeId)), nil
		}

		// Return the full definition minus the factory function (already excluded via json:"-")
		return jsonResult(def), nil
	}
}
