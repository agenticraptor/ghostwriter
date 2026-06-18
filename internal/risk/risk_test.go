package risk

import (
	"testing"

	"github.com/agenticraptor/ghostwriter/internal/gitdiff"
)

func hasLabel(flags []Flag, label string) bool {
	for _, f := range flags {
		if f.Label == label {
			return true
		}
	}
	return false
}

func TestMigrationFlag(t *testing.T) {
	f := gitdiff.File{NewPath: "db/migrations/008_orders.sql", Status: gitdiff.Added}
	flags := Analyze(f)
	if !hasLabel(flags, "migration") {
		t.Fatalf("expected migration flag, got %+v", flags)
	}
	if Max(flags) != High {
		t.Errorf("expected High severity, got %v", Max(flags))
	}
}

func TestLockfileAndManifest(t *testing.T) {
	if !hasLabel(Analyze(gitdiff.File{NewPath: "package-lock.json", Status: gitdiff.Modified}), "lockfile") {
		t.Error("expected lockfile flag")
	}
	if !hasLabel(Analyze(gitdiff.File{NewPath: "go.mod", Status: gitdiff.Modified}), "deps") {
		t.Error("expected deps flag")
	}
}

func TestSecretDetection(t *testing.T) {
	f := gitdiff.File{
		NewPath: "config.go",
		Status:  gitdiff.Modified,
		Hunks: []gitdiff.Hunk{{
			Lines: []gitdiff.Line{
				{Kind: gitdiff.Context, Text: "package main"},
				{Kind: gitdiff.Insert, Text: `apiKey = "AKIAIOSFODNN7EXAMPLE"`},
			},
		}},
	}
	if !hasLabel(Analyze(f), "secret") {
		t.Fatalf("expected secret flag, got %+v", Analyze(f))
	}
}

func TestDeletedTestFile(t *testing.T) {
	f := gitdiff.File{OldPath: "pkg/checkout_test.go", Status: gitdiff.Deleted}
	flags := Analyze(f)
	if !hasLabel(flags, "deletion") {
		t.Fatalf("expected deletion flag, got %+v", flags)
	}
}

func TestCleanFileHasNoFlags(t *testing.T) {
	f := gitdiff.File{
		NewPath: "internal/util/strings.go",
		Status:  gitdiff.Modified,
		Hunks: []gitdiff.Hunk{{
			Lines: []gitdiff.Line{{Kind: gitdiff.Insert, Text: "return strings.TrimSpace(s)"}},
		}},
	}
	if flags := Analyze(f); len(flags) != 0 {
		t.Errorf("expected no flags, got %+v", flags)
	}
}
