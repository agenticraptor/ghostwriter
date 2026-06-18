# Configuration

ghostwriter runs fine with **no configuration at all**. The optional config file
only lets you persist preferences so you don't have to pass flags every time.

## Location

The file lives at:

| Platform | Path |
|----------|------|
| Linux / macOS | `$XDG_CONFIG_HOME/ghostwriter/config.toml` or `~/.config/ghostwriter/config.toml` |
| Override | set `GHOSTWRITER_CONFIG=/path/to/config.toml` |

Run `ghostwriter config path` to print the exact location, and
`ghostwriter config init` to write a documented starter file (it never
overwrites an existing one).

## Reference

```toml
[ai]
# provider: anthropic | openai | ollama.
# Leave empty to auto-detect from your environment:
#   ANTHROPIC_API_KEY set -> anthropic
#   OPENAI_API_KEY set    -> openai
#   otherwise             -> a local Ollama at http://localhost:11434
provider = ""

# model: leave empty to use the provider's default
#   anthropic -> claude-sonnet-4-6
#   openai    -> gpt-4o-mini
#   ollama    -> llama3.1
model = ""

# enabled: set to false to ALWAYS use the offline heuristic grouping,
# even when an API key is present.
enabled = true

# max_diff_bytes: caps how much diff text is sent to the model. Larger diffs are
# truncated (the hunk indices are still listed so they can be referenced).
max_diff_bytes = 14000

[review]
# against: the git ref the working tree is compared against.
against = "HEAD"

# include_untracked: also review brand-new, untracked files.
include_untracked = true
```

## Precedence

For any given setting, the value is resolved in this order (first wins):

1. A command-line flag (e.g. `--provider`, `--model`, `--against`, `--max-bytes`).
2. The config file.
3. The built-in default.

Environment variables are used only for **credentials** (`ANTHROPIC_API_KEY`,
`OPENAI_API_KEY`) and for `GHOSTWRITER_MODEL` (a convenience override for the
model name when auto-detecting). Keys are never written to the config file.

## Environment variables

| Variable | Effect |
|----------|--------|
| `ANTHROPIC_API_KEY` | Enables and selects the Anthropic provider when auto-detecting. |
| `OPENAI_API_KEY` | Enables and selects the OpenAI provider when auto-detecting. |
| `GHOSTWRITER_MODEL` | Overrides the model name during auto-detection. |
| `OLLAMA_HOST` | Base URL for a non-default Ollama instance. |
| `GHOSTWRITER_CONFIG` | Absolute path to the config file. |
| `COLUMNS` | Width hint used when wrapping non-interactive output. |
