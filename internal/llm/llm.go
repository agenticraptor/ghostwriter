// Package llm provides a tiny, dependency-free client for the chat APIs of
// Anthropic, OpenAI, and Ollama. It speaks plain HTTP+JSON rather than pulling
// in a vendor SDK, keeping the binary small.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Sentinel errors.
var (
	// ErrNoCredentials means the provider needs an API key that is not set.
	ErrNoCredentials = errors.New("no API credentials found in environment")
	// ErrUnknownProvider means the configured provider name is unrecognized.
	ErrUnknownProvider = errors.New("unknown provider")
)

// Request is a single completion request.
type Request struct {
	System      string
	Prompt      string
	MaxTokens   int
	Temperature float64
}

// Client is a minimal chat-completion client.
type Client interface {
	// Name returns the provider identifier (e.g. "anthropic").
	Name() string
	// Model returns the resolved model name.
	Model() string
	// Complete returns the assistant's text response.
	Complete(ctx context.Context, req Request) (string, error)
}

// New constructs a client for the named provider. model and baseURL may be
// empty to use sensible defaults. Cloud providers require an API key in the
// environment and return ErrNoCredentials if it is missing.
func New(provider, model, baseURL string) (Client, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "anthropic", "claude":
		return newAnthropic(model, baseURL)
	case "openai", "gpt":
		return newOpenAI(model, baseURL)
	case "ollama", "local":
		return newOllama(model, baseURL)
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownProvider, provider)
	}
}

// Detect picks a provider from the environment: Anthropic if ANTHROPIC_API_KEY
// is set, otherwise OpenAI if OPENAI_API_KEY is set, otherwise Ollama. The
// model may be overridden by GHOSTWRITER_MODEL.
func Detect() (Client, error) {
	model := os.Getenv("GHOSTWRITER_MODEL")
	switch {
	case os.Getenv("ANTHROPIC_API_KEY") != "":
		return New("anthropic", model, "")
	case os.Getenv("OPENAI_API_KEY") != "":
		return New("openai", model, "")
	default:
		return New("ollama", model, "")
	}
}

var httpClient = &http.Client{Timeout: 180 * time.Second}

// postJSON marshals payload, POSTs it, and decodes a successful response into
// out. Non-2xx responses become errors that include a short body snippet.
func postJSON(ctx context.Context, url string, headers map[string]string, payload, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s: %s", resp.Status, snippet(data))
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}
