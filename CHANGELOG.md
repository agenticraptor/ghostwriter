# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-06-15

### Added

- Initial release. 🎉
- Intent-grouped review of the changes in your git working tree (tracked edits
  plus new, untracked files).
- AI narration that clusters related hunks across files into plain-English
  intents, provider-agnostic across Anthropic, OpenAI, and local Ollama, selected
  from configuration or the environment.
- Deterministic, zero-config offline grouping (one intent per file) that works
  with no API key, including heuristic risk flags for migrations, lockfiles,
  dependency manifests, CI/Docker, infrastructure-as-code, possible secrets, and
  risky or test-removing deletions.
- Dependency change detection across npm, Go, pip, and Cargo.
- Interactive Bubble Tea reviewer with one-key accept/reject per intent.
- Safe application of rejections via a checked `git apply --reverse` (and file
  deletion for rejected new files); a change that cannot be cleanly reverted is
  reported, never half-applied. A partial reject of a renamed file preserves the
  rename instead of undoing it.
- Reject safety net: every rejection is backed up to
  `<git-dir>/ghostwriter/rejected-<timestamp>.patch` before it is applied, so
  reverted work can always be restored with `git apply`.
- Security hardening: untracked symlinks are never followed (no file-exfiltration
  via a malicious repo), `--against` refs are validated and all git calls
  terminate options with `--` (no argument injection), reverts refuse paths
  outside the repository, and secret-looking lines are redacted before any diff
  text is sent to a cloud model.
- Works in a repository with no commits yet (diffs against the empty tree).
- Output formats: styled terminal, Markdown, JSON, and plain text.
- `config`, `doctor`, and `version` commands.

[Unreleased]: https://github.com/agenticraptor/ghostwriter/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/agenticraptor/ghostwriter/releases/tag/v0.1.0
