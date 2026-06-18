package llm

import (
	"context"
	"errors"
	"os"
	"strings"
)

// DefaultOpenAIModel is a small, inexpensive default suited to summarization.
const DefaultOpenAIModel = "gpt-4o-mini"

type openaiClient struct {
	apiKey  string
	model   string
	baseURL string
}

func newOpenAI(model, baseURL string) (Client, error) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		return nil, ErrNoCredentials
	}
	if model == "" {
		model = DefaultOpenAIModel
	}
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	return &openaiClient{apiKey: key, model: model, baseURL: strings.TrimRight(baseURL, "/")}, nil
}

func (c *openaiClient) Name() string  { return "openai" }
func (c *openaiClient) Model() string { return c.model }

func (c *openaiClient) Complete(ctx context.Context, req Request) (string, error) {
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	messages := []map[string]any{}
	if req.System != "" {
		messages = append(messages, map[string]any{"role": "system", "content": req.System})
	}
	messages = append(messages, map[string]any{"role": "user", "content": req.Prompt})

	payload := map[string]any{
		"model":       c.model,
		"max_tokens":  maxTokens,
		"temperature": req.Temperature,
		"messages":    messages,
	}
	headers := map[string]string{"Authorization": "Bearer " + c.apiKey}

	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := postJSON(ctx, c.baseURL+"/v1/chat/completions", headers, payload, &out); err != nil {
		return "", err
	}
	if out.Error != nil {
		return "", errors.New(out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return "", errors.New("empty response from openai")
	}
	return strings.TrimSpace(out.Choices[0].Message.Content), nil
}
