package llm

import (
	"context"
	"errors"
	"os"
	"strings"
)

// DefaultOllamaModel is a reasonable local default; override with --model.
const DefaultOllamaModel = "llama3.1"

type ollamaClient struct {
	model   string
	baseURL string
}

func newOllama(model, baseURL string) (Client, error) {
	if model == "" {
		model = DefaultOllamaModel
	}
	if baseURL == "" {
		baseURL = os.Getenv("OLLAMA_HOST")
	}
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	if !strings.HasPrefix(baseURL, "http") {
		baseURL = "http://" + baseURL
	}
	return &ollamaClient{model: model, baseURL: strings.TrimRight(baseURL, "/")}, nil
}

func (c *ollamaClient) Name() string  { return "ollama" }
func (c *ollamaClient) Model() string { return c.model }

func (c *ollamaClient) Complete(ctx context.Context, req Request) (string, error) {
	messages := []map[string]any{}
	if req.System != "" {
		messages = append(messages, map[string]any{"role": "system", "content": req.System})
	}
	messages = append(messages, map[string]any{"role": "user", "content": req.Prompt})

	payload := map[string]any{
		"model":    c.model,
		"messages": messages,
		"stream":   false,
		"options": map[string]any{
			"temperature": req.Temperature,
		},
	}

	var out struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		Error string `json:"error"`
	}
	if err := postJSON(ctx, c.baseURL+"/api/chat", nil, payload, &out); err != nil {
		return "", err
	}
	if out.Error != "" {
		return "", errors.New(out.Error)
	}
	return strings.TrimSpace(out.Message.Content), nil
}
