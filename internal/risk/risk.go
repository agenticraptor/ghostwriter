// Package risk applies fast, deterministic heuristics to a changed file and
// flags the kinds of changes a reviewer should look at twice — migrations,
// dependency and lockfile edits, CI/Docker changes, possible secrets, and large
// or test-removing deletions. These flags never require a network or a model,
// so they work even with --no-ai.
package risk

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/agenticraptor/ghostwriter/internal/gitdiff"
)

// Severity ranks how much attention a flag deserves.
type Severity int

// Severity levels.
const (
	Info Severity = iota
	Warn
	High
)

func (s Severity) String() string {
	switch s {
	case High:
		return "high"
	case Warn:
		return "warn"
	default:
		return "info"
	}
}

// Flag is a single risk observation about a file.
type Flag struct {
	Severity Severity
	Label    string // short tag, e.g. "migration"
	Reason   string // one-line human explanation
}

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(api[_-]?key|secret|client[_-]?secret|access[_-]?token|auth[_-]?token|password|passwd)\s*[:=]\s*['"]?[A-Za-z0-9_\-./+=]{8,}`),
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),
	regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9_\-.=]{16,}`),
	regexp.MustCompile(`xox[baprs]-[0-9A-Za-z-]{10,}`),
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{20,}`),
}

// Analyze returns the risk flags for a single changed file.
func Analyze(f gitdiff.File) []Flag {
	var flags []Flag
	path := f.Path()
	base := strings.ToLower(filepath.Base(path))
	lower := strings.ToLower(path)

	if isMigration(lower, base) {
		flags = append(flags, Flag{High, "migration", "Database migration — review before it runs against real data."})
	}
	if isLockfile(base) {
		flags = append(flags, Flag{Warn, "lockfile", "Dependency lockfile changed — transitive dependencies may have moved."})
	}
	if isManifest(base) {
		flags = append(flags, Flag{Info, "deps", "Dependency manifest changed."})
	}
	if isCI(lower, base) {
		flags = append(flags, Flag{Warn, "ci/build", "CI or build configuration changed."})
	}
	if isInfra(lower, base) {
		flags = append(flags, Flag{Warn, "infra", "Infrastructure-as-code changed."})
	}
	if isEnvFile(base) {
		flags = append(flags, Flag{High, "env", "Environment file changed — check for committed secrets."})
	}
	if f.Status == gitdiff.Deleted {
		sev := Warn
		reason := "File deleted."
		if isTest(lower) {
			reason = "Test file deleted — coverage may have dropped."
		}
		flags = append(flags, Flag{sev, "deletion", reason})
	} else if f.Deletions >= 80 && f.Deletions > f.Additions*2 {
		flags = append(flags, Flag{Warn, "large-deletion", "Large net deletion — make sure nothing important was removed."})
	}
	if label, reason, ok := secretScan(f); ok {
		flags = append(flags, Flag{High, label, reason})
	}
	if touchesAuth(lower) {
		flags = append(flags, Flag{Info, "security", "Touches authentication / security code."})
	}
	return dedupe(flags)
}

// Max returns the highest severity among the flags (Info if there are none).
func Max(flags []Flag) Severity {
	m := Info
	for _, f := range flags {
		if f.Severity > m {
			m = f.Severity
		}
	}
	return m
}

func isMigration(lower, base string) bool {
	if strings.Contains(lower, "/migrations/") || strings.Contains(lower, "/migrate/") {
		return true
	}
	if strings.HasSuffix(base, ".sql") {
		return true
	}
	return strings.Contains(base, "migration")
}

func isLockfile(base string) bool {
	switch base {
	case "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "go.sum",
		"cargo.lock", "poetry.lock", "gemfile.lock", "composer.lock",
		"pdm.lock", "uv.lock", "bun.lockb":
		return true
	}
	return false
}

func isManifest(base string) bool {
	switch base {
	case "package.json", "go.mod", "requirements.txt", "cargo.toml",
		"pyproject.toml", "gemfile", "build.gradle", "build.gradle.kts",
		"pom.xml", "composer.json", "pipfile":
		return true
	}
	return false
}

func isCI(lower, base string) bool {
	if strings.Contains(lower, ".github/workflows/") || strings.Contains(lower, ".gitlab-ci") {
		return true
	}
	switch base {
	case "dockerfile", "docker-compose.yml", "docker-compose.yaml",
		"makefile", "jenkinsfile", ".travis.yml", "azure-pipelines.yml":
		return true
	}
	return strings.HasPrefix(base, "dockerfile")
}

func isInfra(lower, base string) bool {
	if strings.HasSuffix(base, ".tf") || strings.HasSuffix(base, ".tfvars") {
		return true
	}
	if strings.HasSuffix(base, ".tfstate") {
		return true
	}
	return strings.Contains(lower, "/k8s/") || strings.Contains(lower, "/kubernetes/") ||
		strings.HasSuffix(base, "values.yaml") && strings.Contains(lower, "chart")
}

func isEnvFile(base string) bool {
	return base == ".env" || strings.HasPrefix(base, ".env.") || strings.HasSuffix(base, ".env")
}

func isTest(lower string) bool {
	return strings.Contains(lower, "_test.") || strings.Contains(lower, ".test.") ||
		strings.Contains(lower, ".spec.") || strings.Contains(lower, "/tests/") ||
		strings.Contains(lower, "/test/") || strings.HasPrefix(filepath.Base(lower), "test_")
}

func touchesAuth(lower string) bool {
	for _, kw := range []string{"auth", "login", "session", "password", "crypto", "jwt", "oauth", "permission"} {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// LooksSecret reports whether a single line appears to contain a secret. It is
// used both for risk flagging and to redact such lines before any diff text is
// sent to a cloud model.
func LooksSecret(line string) bool {
	for _, re := range secretPatterns {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

// secretScan looks for likely secrets among the inserted lines of the file.
func secretScan(f gitdiff.File) (label, reason string, ok bool) {
	for _, h := range f.Hunks {
		for _, ln := range h.Lines {
			if ln.Kind != gitdiff.Insert {
				continue
			}
			if LooksSecret(ln.Text) {
				return "secret", "A line looks like a hard-coded secret or key.", true
			}
		}
	}
	return "", "", false
}

func dedupe(flags []Flag) []Flag {
	seen := map[string]bool{}
	out := flags[:0]
	for _, f := range flags {
		if seen[f.Label] {
			continue
		}
		seen[f.Label] = true
		out = append(out, f)
	}
	return out
}
