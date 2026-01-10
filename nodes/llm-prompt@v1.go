package nodes

import (
	"context"
	_ "embed"
	"io"
	"net/http"
	"strings"

	"github.com/actionforge/actrun-cli/core" // Assuming node_interfaces are generated

	ni "github.com/actionforge/actrun-cli/node_interfaces"

	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/anthropic"
	"github.com/tmc/langchaingo/llms/googleai"
	"github.com/tmc/langchaingo/llms/mistral"
	"github.com/tmc/langchaingo/llms/ollama"
	"github.com/tmc/langchaingo/llms/openai"
)

//go:embed llm-prompt@v1.yml
var llmGenerateDefinition string

// LlmGenerateNode is an execution node that generates text from an LLM.
type LlmGenerateNode struct {
	core.NodeBaseComponent
	core.Executions
	core.Inputs
	core.Outputs
}

func createLlmClient(model, apiKey string) (llms.Model, error) {
	modelLower := strings.ToLower(model)

	switch {
	case strings.HasPrefix(modelLower, "gpt-") || strings.HasPrefix(modelLower, "o1-") || strings.HasPrefix(modelLower, "o2-") || strings.HasPrefix(modelLower, "o3-"):
		if apiKey == "" {
			return nil, core.CreateErr(nil, nil, "An API key is required for OpenAI models")
		}

		opts := []openai.Option{
			openai.WithModel(model),
			openai.WithToken(apiKey),
		}
		return openai.New(opts...)

	case strings.HasPrefix(modelLower, "grok-"):
		if apiKey == "" {
			return nil, core.CreateErr(nil, nil, "An API key is required for Grok models")
		}

		return openai.New(
			openai.WithBaseURL("https://api.x.ai/v1"),
			openai.WithModel(model),
			openai.WithToken(apiKey),
		)

	case strings.HasPrefix(modelLower, "claude-"):
		if apiKey == "" {
			return nil, core.CreateErr(nil, nil, "An API key is required for Anthropic models")
		}

		return anthropic.New(
			anthropic.WithModel(model),
			anthropic.WithToken(apiKey),
		)

	case strings.HasPrefix(modelLower, "gemini-"):
		if apiKey == "" {
			return nil, core.CreateErr(nil, nil, "An API key is required for Gemini models")
		}

		return googleai.New(
			context.Background(),
			googleai.WithAPIKey(apiKey),
			googleai.WithDefaultModel(model),
		)

	case strings.HasPrefix(modelLower, "mistral-") || strings.HasPrefix(modelLower, "mixtral-"):
		if apiKey == "" {
			return nil, core.CreateErr(nil, nil, "An API key is required for Mistral models")
		}

		return mistral.New(
			mistral.WithModel(model),
			mistral.WithAPIKey(apiKey),
		)

	default:
		opts := []ollama.Option{ollama.WithModel(model)}
		return ollama.New(opts...)
	}
}

func (n *LlmGenerateNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	systemPrompt, err := core.InputValueById[string](c, n, ni.Core_llm_prompt_v1_Input_user_prompt)
	if err != nil {
		return err
	}

	userPrompt, err := core.InputValueById[string](c, n, ni.Core_llm_prompt_v1_Input_system_prompt)
	if err != nil {
		return err
	}

	model, err := core.InputValueById[string](c, n, ni.Core_llm_prompt_v1_Input_model)
	if err != nil {
		return err
	}

	apiKey, err := core.InputValueById[core.SecretValue](c, n, ni.Core_llm_prompt_v1_Input_api_key)
	if err != nil {
		return err
	}

	llm, err := createLlmClient(model, apiKey.Secret)
	if err != nil {
		return err
	}

	attachments, err := core.InputArrayValueById[io.Reader](c, n, ni.Core_llm_prompt_v1_Input_Attachments, core.GetInputValueOpts{})
	if err != nil {
		return err
	}

	humanParts := []llms.ContentPart{
		llms.TextPart(userPrompt),
	}

	for i, attachmentReader := range attachments {
		// Read the content from the io.Reader
		data, err := io.ReadAll(attachmentReader)
		if err != nil {
			return core.CreateErr(nil, err, "failed to read attachment data at input %d", i)
		}

		mimeType := http.DetectContentType(data)

		// Create a BinaryPart and append it to the human message parts
		humanParts = append(humanParts, llms.BinaryPart(mimeType, data))
	}

	messages := []llms.MessageContent{
		{
			Role: llms.ChatMessageTypeSystem,
			Parts: []llms.ContentPart{
				llms.TextPart(systemPrompt),
			},
		},
		{
			Role:  llms.ChatMessageTypeHuman,
			Parts: humanParts,
		},
	}

	response, err := llm.GenerateContent(context.Background(),
		messages,
		llms.WithTemperature(defaultTemperature(model)),
	)
	if err != nil {
		err := core.CreateErr(c, err, "failed to generate text from LLM")
		return n.Execute(ni.Core_llm_prompt_v1_Output_exec_err, c, err)
	}

	var outputText string
	if response != nil && len(response.Choices) > 0 {
		outputText = response.Choices[0].Content
	} else {
		outputText = ""
	}

	err = n.SetOutputValue(c, ni.Core_llm_prompt_v1_Output_response, outputText, core.SetOutputValueOpts{})
	if err != nil {
		return n.Execute(ni.Core_llm_prompt_v1_Output_exec_err, c, err)
	}

	return n.Execute(ni.Core_llm_prompt_v1_Output_exec_success, c, nil)
}

func defaultTemperature(model string) float64 {
	modelLower := strings.ToLower(model)

	switch {
	case strings.HasPrefix(modelLower, "gpt-5"):
		return 1.0
	case strings.HasPrefix(modelLower, "gpt-"),
		strings.HasPrefix(modelLower, "o1-"),
		strings.HasPrefix(modelLower, "o2-"),
		strings.HasPrefix(modelLower, "o3-"),
		strings.HasPrefix(modelLower, "grok-"),
		strings.HasPrefix(modelLower, "gemini-"):
		return 0.7
	case strings.HasPrefix(modelLower, "claude-"):
		return 1.0
	case strings.HasPrefix(modelLower, "mistral-"), strings.HasPrefix(modelLower, "mixtral-"):
		return 0.8
	default:
		// Ollama fallback or unknown models
		return 0.7
	}
}

func init() {
	err := core.RegisterNodeFactory(llmGenerateDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &LlmGenerateNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
