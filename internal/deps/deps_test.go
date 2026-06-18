package deps

import (
	"testing"

	"github.com/agenticraptor/ghostwriter/internal/gitdiff"
)

func fileWith(path string, lines ...gitdiff.Line) gitdiff.File {
	return gitdiff.File{NewPath: path, Status: gitdiff.Modified, Hunks: []gitdiff.Hunk{{Lines: lines}}}
}

func TestNpmAddRemove(t *testing.T) {
	f := fileWith("package.json",
		gitdiff.Line{Kind: gitdiff.Insert, Text: `    "left-pad": "^1.3.0",`},
		gitdiff.Line{Kind: gitdiff.Delete, Text: `    "lodash": "^4.17.21",`},
		gitdiff.Line{Kind: gitdiff.Insert, Text: `    "name": "my-app",`}, // not a version → ignored
	)
	got := Analyze(gitdiff.Diff{Files: []gitdiff.File{f}})
	if len(got) != 2 {
		t.Fatalf("expected 2 changes, got %d: %+v", len(got), got)
	}
	var added, removed *Change
	for i := range got {
		if got[i].Added {
			added = &got[i]
		} else {
			removed = &got[i]
		}
	}
	if added == nil || added.Name != "left-pad" || added.Version != "^1.3.0" {
		t.Errorf("added = %+v", added)
	}
	if removed == nil || removed.Name != "lodash" {
		t.Errorf("removed = %+v", removed)
	}
}

func TestGoModDep(t *testing.T) {
	f := fileWith("go.mod",
		gitdiff.Line{Kind: gitdiff.Insert, Text: "\tgithub.com/spf13/cobra v1.8.0"},
	)
	got := Analyze(gitdiff.Diff{Files: []gitdiff.File{f}})
	if len(got) != 1 || got[0].Name != "github.com/spf13/cobra" || got[0].Version != "v1.8.0" {
		t.Fatalf("go dep parse = %+v", got)
	}
	if got[0].Ecosystem != "go" {
		t.Errorf("ecosystem = %q", got[0].Ecosystem)
	}
}

func TestPipDep(t *testing.T) {
	f := fileWith("requirements.txt",
		gitdiff.Line{Kind: gitdiff.Insert, Text: "requests==2.31.0"},
		gitdiff.Line{Kind: gitdiff.Insert, Text: "# a comment"}, // ignored
	)
	got := Analyze(gitdiff.Diff{Files: []gitdiff.File{f}})
	if len(got) != 1 || got[0].Name != "requests" || got[0].Version != "==2.31.0" {
		t.Fatalf("pip dep parse = %+v", got)
	}
}

func TestNonManifestIgnored(t *testing.T) {
	f := fileWith("main.go", gitdiff.Line{Kind: gitdiff.Insert, Text: `"left-pad": "^1.3.0"`})
	if got := Analyze(gitdiff.Diff{Files: []gitdiff.File{f}}); len(got) != 0 {
		t.Errorf("expected no deps from a .go file, got %+v", got)
	}
}
