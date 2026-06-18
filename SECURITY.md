# Security Policy

## Supported versions

The latest released minor version receives security fixes. Please upgrade to the
most recent release before reporting an issue.

## Reporting a vulnerability

Please **do not** open a public issue for security problems.

Instead, report it privately through GitHub's built-in
[**private vulnerability reporting**](https://github.com/agenticraptor/ghostwriter/security/advisories/new).
This keeps the report confidential between you and the maintainers until a fix is
released.

Please include:

- A description of the issue and its impact.
- Steps to reproduce (a minimal proof of concept is ideal).
- Affected version(s) and platform.

We aim to acknowledge reports within **72 hours** and to provide a remediation
timeline after triage. We will credit reporters in the release notes unless you
prefer to remain anonymous.

## Scope & data handling notes

ghostwriter is a local tool that operates on your git working tree. A few things
worth knowing for your own threat model:

- **API keys** are read from environment variables (`ANTHROPIC_API_KEY`,
  `OPENAI_API_KEY`) and are never written to the config file.
- **Cloud models:** when an Anthropic/OpenAI provider is selected, a
  *size-capped* sample of your diff (file paths and changed lines, up to
  `max_diff_bytes`) is sent to that provider to generate the narrative. Use
  `--no-ai` or `--provider ollama` to keep all data local.
- **Secret redaction:** lines that look like secrets (API keys, tokens, private
  keys) are replaced with `[redacted possible secret]` *before* any diff text is
  sent to a cloud model, so a stray credential is not transmitted. The
  deterministic risk flag still fires locally.
- **Symlinks are never followed.** Untracked symbolic links are listed but their
  targets are never read, so a malicious repository cannot use a link (e.g. to
  `/etc/passwd` or `~/.ssh/id_rsa`) to leak files into the review or to a model.
- **Refs are validated.** The `--against` value is rejected if it begins with
  `-`, and all git invocations terminate options with `--`, so a crafted ref or
  config value can never be smuggled in as a git option (e.g. `--output=`).
- **Applying rejections** is a purely local `git apply --reverse` operation. It
  is run with a dry-run check first, refuses to touch paths outside the
  repository, and never executes arbitrary code from the diff or the model.
- **Reverts are backed up.** Before any rejection is applied, the affected change
  is written to `<git-dir>/ghostwriter/rejected-<timestamp>.patch` so it can be
  restored with `git apply`, even if it was never committed.
- **Secret detection is best-effort and heuristic.** ghostwriter may *flag* or
  *redact* a line that looks like a secret, but a missing flag is **not** a
  guarantee that no secret is present — always review sensitive changes yourself.
