package review

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agenticraptor/ghostwriter/internal/gitdiff"
	"github.com/agenticraptor/ghostwriter/internal/intent"
	"github.com/agenticraptor/ghostwriter/internal/risk"
)

// TestBuildAndPartialRevert is the headline integration test: it simulates the
// changes an AI agent makes, builds an offline review, then rejects two of the
// intents (a dependency bump and a risky migration) while keeping the rest —
// confirming the kept changes survive and the rejected ones are undone.
func TestBuildAndPartialRevert(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	ctx := context.Background()
	dir := t.TempDir()
	git := gitRunner(t, dir)

	git("init", "-q", "-b", "main")
	git("config", "user.email", "t@e.com")
	git("config", "user.name", "T")
	git("config", "core.autocrlf", "false")

	writeFile(t, dir, "client.go", "package pay\n\nfunc Charge() error {\n\treturn do()\n}\n")
	writeFile(t, dir, "go.mod", "module example.com/api\n\ngo 1.22\n\nrequire github.com/stripe/stripe-go/v76 v76.10.0\n")
	git("add", "-A")
	git("commit", "-q", "-m", "baseline")

	// The "agent" makes four kinds of change.
	writeFile(t, dir, "client.go", "package pay\n\nfunc Charge() error {\n\tfor i := 0; i < 3; i++ {\n\t\tif err := do(); err == nil {\n\t\t\treturn nil\n\t\t}\n\t}\n\treturn errFailed\n}\n")
	writeFile(t, dir, "go.mod", "module example.com/api\n\ngo 1.22\n\nrequire github.com/stripe/stripe-go/v76 v76.25.0\n")
	mkdir(t, dir, "migrations")
	writeFile(t, dir, "migrations/001_add_col.sql", "ALTER TABLE charges ADD COLUMN idem TEXT;\n")
	writeFile(t, dir, "client_test.go", "package pay\n\nimport \"testing\"\n\nfunc TestCharge(t *testing.T) {}\n")

	rev, err := Build(ctx, Options{Repo: dir, NoAI: true})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	if files, _, _ := rev.Totals(); files != 4 {
		t.Fatalf("expected 4 changed files, got %d", files)
	}
	if len(rev.Deps) == 0 {
		t.Error("expected a dependency change to be detected in go.mod")
	}
	if !hasHighRiskMigration(rev) {
		t.Error("expected a high-severity migration risk flag")
	}

	// Reject the go.mod and migration intents; keep client.go and the test.
	merged := map[int][]int{}
	rejected := 0
	for _, in := range rev.Intents {
		if strings.Contains(in.Title, "go.mod") || strings.Contains(in.Title, ".sql") {
			rejected++
			for fi, hs := range intent.FileHunks(in) {
				merged[fi] = hs
			}
		}
	}
	if rejected != 2 {
		t.Fatalf("expected to reject 2 intents, matched %d", rejected)
	}

	for _, r := range gitdiff.Revert(ctx, rev.Root, rev.Diff, merged) {
		if !r.OK {
			t.Fatalf("revert %s failed: %v", r.Path, r.Err)
		}
	}

	// Kept: client.go retry loop and the new test file.
	if got := readFile(t, dir, "client.go"); !strings.Contains(got, "for i := 0; i < 3") {
		t.Errorf("client.go retry logic should have been kept, got:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "client_test.go")); err != nil {
		t.Errorf("client_test.go should have been kept: %v", err)
	}
	// Reverted: go.mod back to baseline, migration removed.
	if got := readFile(t, dir, "go.mod"); !strings.Contains(got, "v76.10.0") {
		t.Errorf("go.mod should be back to baseline v76.10.0, got:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "migrations/001_add_col.sql")); !os.IsNotExist(err) {
		t.Errorf("migration should have been deleted, stat err = %v", err)
	}
}

func TestBuildEmptyRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	ctx := context.Background()
	dir := t.TempDir()
	git := gitRunner(t, dir)
	git("init", "-q", "-b", "main")
	git("config", "user.email", "t@e.com")
	git("config", "user.name", "T")
	git("config", "core.autocrlf", "false")
	writeFile(t, dir, "a.txt", "hi\n")
	git("add", "-A")
	git("commit", "-q", "-m", "init")

	rev, err := Build(ctx, Options{Repo: dir, NoAI: true})
	if err != nil {
		t.Fatal(err)
	}
	if !rev.Empty() {
		t.Errorf("expected empty review on a clean repo, got %d intents", len(rev.Intents))
	}
}

func hasHighRiskMigration(rev *Review) bool {
	for _, in := range rev.Intents {
		for _, fl := range in.Risks {
			if fl.Label == "migration" && fl.Severity == risk.High {
				return true
			}
		}
	}
	return false
}

func gitRunner(t *testing.T, dir string) func(args ...string) {
	t.Helper()
	return func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@e.com",
			"GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@e.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	full := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mkdir(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
