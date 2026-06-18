# Model providers

ghostwriter's narration is **provider-agnostic**. It speaks plain HTTP+JSON to
each provider rather than bundling a vendor SDK, so the binary stays small. If
no provider is available, it falls back to deterministic, offline grouping — the
tool is always useful.

## Auto-detection

With no `--provider` flag and no `provider` in the config, ghostwriter picks one
from your environment:

1. `ANTHROPIC_API_KEY` is set → **Anthropic**
2. else `OPENAI_API_KEY` is set → **OpenAI**
3. else → **Ollama** at `http://localhost:11434`

If the chosen provider is unreachable or unauthorized, ghostwriter prints a short
notice and shows the offline heuristic grouping instead.

## Anthropic

```bash
export ANTHROPIC_API_KEY=sk-ant-...
ghostwriter                       # uses claude-sonnet-4-6 by default
ghostwriter --model claude-opus-4-8
```

## OpenAI

```bash
export OPENAI_API_KEY=sk-...
ghostwriter --provider openai     # uses gpt-4o-mini by default
ghostwriter --provider openai --model gpt-4o
```

## Ollama (100% local)

No API key, no egress. Install [Ollama](https://ollama.com), pull a model, and
point ghostwriter at it:

```bash
ollama pull llama3.1
ghostwriter --provider ollama --model llama3.1
```

Use a non-default host with `OLLAMA_HOST`:

```bash
OLLAMA_HOST=http://192.168.1.50:11434 ghostwriter --provider ollama
```

## Offline (no model at all)

```bash
ghostwriter --no-ai
```

This groups the diff one-intent-per-file and keeps every risk flag and
dependency-change detection intact. It's instant, free, and never touches the
network — ideal for CI or air-gapped machines.

## Choosing a model

Narration is a summarization task, so a small, fast model is usually plenty. The
defaults (`claude-sonnet-4-6`, `gpt-4o-mini`, `llama3.1`) are chosen to be
cost-effective. Reach for a larger model only if you find the intent grouping on
big, tangled diffs isn't sharp enough.
