# Contributing to ghostwriter

Thanks for your interest in contributing! This project aims to be a small,
focused, dependency-light tool — contributions that keep it that way are
especially appreciated.

## Getting started

```bash
git clone https://github.com/agenticraptor/ghostwriter
cd ghostwriter
go mod tidy        # fetch dependencies & populate go.sum
make build         # build into ./bin/ghostwriter
make test          # run the unit tests
```

Requirements:

- Go 1.22 or newer
- `git` on your `PATH` (ghostwriter shells out to it)
- (optional) [`golangci-lint`](https://golangci-lint.run/) for `make lint`
- (optional) [`goreleaser`](https://goreleaser.com/) for `make snapshot`

## Development workflow

1. Fork the repo and create a feature branch from `main`.
2. Make your change, with tests where it makes sense.
3. Run the full check suite locally:
   ```bash
   make fmt vet test
   ```
4. Open a pull request. Fill in the PR template and link any related issue.

CI runs `gofmt`, `go vet`, `golangci-lint`, and the test suite on Linux, macOS,
and Windows. All checks must pass before review.

## Commit messages

We use [Conventional Commits](https://www.conventionalcommits.org/). This keeps
the generated changelog readable and drives semantic-version bumps.

```
feat: add Cargo dependency detection
fix: handle CRLF line endings in the diff parser
docs: clarify the Ollama setup steps
test: cover partial reject across multiple files
chore: bump bubbletea to v0.27.2
```

## Coding guidelines

- **Keep dependencies minimal.** This tool intentionally ships with only a
  handful of direct dependencies. Prefer the standard library; if a new
  dependency is truly needed, call it out in the PR description.
- **Tolerant parsing.** Anything that reads external data (git output, model
  responses) must degrade gracefully — skip the bad record, never crash the
  review. If the model returns junk, fall back to deterministic grouping.
- **Never corrupt the working tree.** The apply path must stay safe: a rejection
  that can't be reverted cleanly is *reported*, never half-applied. Reverts go
  through a checked `git apply --reverse` (dry run first).
- **The offline path is sacred.** The tool must remain useful with no API key.
  If you touch the review pipeline, make sure `--no-ai` still produces a good
  result.
- **Format with `gofmt -s`** and keep `go vet` clean.

## Architecture at a glance

| Package | Responsibility |
|---------|----------------|
| `internal/gitdiff` | Collect & parse the diff; reverse-apply (reject) safely. |
| `internal/intent` | Group hunks into intents (LLM narrator + offline fallback). |
| `internal/risk` | Deterministic risk heuristics over a changed file. |
| `internal/deps` | Detect added/removed dependencies across ecosystems. |
| `internal/llm` | Tiny HTTP clients for Anthropic, OpenAI, and Ollama. |
| `internal/review` | Orchestrate diff → intents → annotation into one `Review`. |
| `internal/render` | Render a `Review` to term / Markdown / JSON / plain. |
| `internal/tui` | Interactive accept/reject (Bubble Tea). |
| `internal/cli` | Cobra commands and the apply flow. |

## Good first issues

- Parse [Cursor](https://www.cursor.com/) / [Aider](https://aider.chat/) session
  history to attribute intents to a specific agent run.
- Add dependency detection for Maven (`pom.xml`), Composer, and RubyGems.
- Add new risk heuristics (e.g. detecting deleted CI jobs, touched `.tfstate`).
- Add a `--html` output format for a shareable, self-contained report.
- A `--reject <ids>` non-interactive flag for scripting.

## Reporting bugs & requesting features

Use the [issue templates](https://github.com/agenticraptor/ghostwriter/issues/new/choose).
For anything security-related, please follow [SECURITY.md](SECURITY.md) instead
of opening a public issue.

## License

By contributing, you agree that your contributions will be licensed under the
[MIT License](LICENSE).
