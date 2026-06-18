// Package config loads ghostwriter's optional TOML configuration. The tool runs
// fine with no config at all; the file only lets you persist preferences such
// as the default model provider or the diff-size budget.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config is the on-disk configuration.
type Config struct {
	AI     AISection     `toml:"ai"`
	Review ReviewSection `toml:"review"`
}

// AISection configures the narration model.
type AISection struct {
	Provider     string `toml:"provider"`       // anthropic | openai | ollama
	Model        string `toml:"model"`          // empty = provider default
	Enabled      bool   `toml:"enabled"`        // false = always use local heuristics
	MaxDiffBytes int    `toml:"max_diff_bytes"` // caps tokens sent to the model
}

// ReviewSection configures what gets reviewed.
type ReviewSection struct {
	Against          string `toml:"against"`           // ref to diff against (default HEAD)
	IncludeUntracked bool   `toml:"include_untracked"` // include new files
}

// Default returns the configuration used when no file is present.
func Default() Config {
	return Config{
		AI:     AISection{Provider: "", Model: "", Enabled: true, MaxDiffBytes: 14000},
		Review: ReviewSection{Against: "HEAD", IncludeUntracked: true},
	}
}

// Path returns the path to the config file, honoring XDG_CONFIG_HOME.
func Path() (string, error) {
	if v := os.Getenv("GHOSTWRITER_CONFIG"); v != "" {
		return v, nil
	}
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "ghostwriter", "config.toml"), nil
}

// Load reads the config file, returning defaults if it does not exist. Unknown
// keys are tolerated so a newer file does not break an older binary.
func Load() (Config, error) {
	cfg := Default()
	path, err := Path()
	if err != nil {
		return cfg, err
	}
	data, err := os.ReadFile(path) //nolint:gosec // path is derived from the user's config dir
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, nil
}

const starter = `# ghostwriter configuration — every field is optional.
# Docs: https://github.com/agenticraptor/ghostwriter

[ai]
# provider: anthropic | openai | ollama. Leave empty to auto-detect from your
# environment (ANTHROPIC_API_KEY -> anthropic, OPENAI_API_KEY -> openai,
# otherwise a local Ollama at http://localhost:11434).
provider = ""
# model: leave empty to use the provider's default.
model = ""
# enabled: set to false to always use the offline heuristic grouping.
enabled = true
# max_diff_bytes: caps how much diff text is sent to the model.
max_diff_bytes = 14000

[review]
# against: the git ref to compare the working tree against.
against = "HEAD"
# include_untracked: also review brand-new, untracked files.
include_untracked = true
`

// Init writes a documented starter config if one does not already exist and
// returns the path. It never overwrites an existing file.
func Init() (string, error) {
	path, err := Path()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(path); err == nil {
		return path, errors.New("config already exists")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(starter), 0o644); err != nil {
		return "", err
	}
	return path, nil
}
