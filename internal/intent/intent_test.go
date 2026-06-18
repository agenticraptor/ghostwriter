package intent

import (
	"strings"
	"testing"

	"github.com/agenticraptor/ghostwriter/internal/gitdiff"
)

func sampleDiff() gitdiff.Diff {
	return gitdiff.Diff{Files: []gitdiff.File{
		{
			NewPath: "pay.go", Status: gitdiff.Modified, Additions: 6, Deletions: 1,
			Hunks: []gitdiff.Hunk{{Additions: 6, Deletions: 1, Section: "func charge()"}},
		},
		{
			NewPath: "pay_test.go", Status: gitdiff.Added, Additions: 12,
			Hunks: []gitdiff.Hunk{{Additions: 12}},
		},
		{
			NewPath: "go.mod", Status: gitdiff.Modified, Additions: 1,
			Hunks: []gitdiff.Hunk{{Additions: 1}},
		},
	}}
}

func TestDeterministicOnePerFile(t *testing.T) {
	intents := Deterministic(sampleDiff())
	if len(intents) != 3 {
		t.Fatalf("expected 3 intents, got %d", len(intents))
	}
	if intents[0].Category != Other {
		t.Errorf("pay.go category = %q, want other", intents[0].Category)
	}
	if intents[1].Category != Test {
		t.Errorf("pay_test.go category = %q, want test", intents[1].Category)
	}
	if intents[2].Category != Deps {
		t.Errorf("go.mod category = %q, want deps", intents[2].Category)
	}
	if intents[0].Additions != 6 || intents[0].Deletions != 1 {
		t.Errorf("counts = +%d/-%d, want +6/-1", intents[0].Additions, intents[0].Deletions)
	}
}

func TestAnnotateComputesFilesAndRisk(t *testing.T) {
	d := gitdiff.Diff{Files: []gitdiff.File{
		{NewPath: "db/migrations/1.sql", Status: gitdiff.Added, Additions: 3, Hunks: []gitdiff.Hunk{{Additions: 3}}},
	}}
	intents := []Intent{{Title: "x", Hunks: []HunkRef{{File: 0, Hunk: 0}}}}
	Annotate(intents, d)
	if len(intents[0].Files) != 1 || intents[0].Files[0] != "db/migrations/1.sql" {
		t.Fatalf("files = %v", intents[0].Files)
	}
	if intents[0].HighestRisk().String() != "high" {
		t.Errorf("expected high risk from migration, got %s", intents[0].HighestRisk())
	}
}

func TestExtractJSON(t *testing.T) {
	cases := []string{
		`{"intents":[]}`,
		"```json\n{\"intents\":[]}\n```",
		"Sure! Here you go:\n{\"intents\":[{\"title\":\"x\"}]}\nHope that helps.",
		"{\"intents\":[{\"title\":\"has } brace in string\"}]}",
	}
	for _, c := range cases {
		got := extractJSON(c)
		if got == "" {
			t.Errorf("extractJSON returned empty for %q", c)
			continue
		}
		if _, err := parseResponse(c); err != nil {
			t.Errorf("parseResponse(%q) error: %v", c, err)
		}
	}
}

func TestParseRef(t *testing.T) {
	d := sampleDiff()
	if ref, ok := parseRef("1:0", d); !ok || ref.File != 1 || ref.Hunk != 0 {
		t.Errorf("parseRef 1:0 = %+v ok=%v", ref, ok)
	}
	if ref, ok := parseRef("F2:H0", d); !ok || ref.File != 2 || ref.Hunk != 0 {
		t.Errorf("parseRef F2:H0 = %+v ok=%v", ref, ok)
	}
	if _, ok := parseRef("9:9", d); ok {
		t.Error("parseRef out-of-range should fail")
	}
}

func TestBuildPromptRedactsSecrets(t *testing.T) {
	d := gitdiff.Diff{Files: []gitdiff.File{{
		NewPath: "config.go", Status: gitdiff.Modified,
		Hunks: []gitdiff.Hunk{{Lines: []gitdiff.Line{
			{Kind: gitdiff.Insert, Text: `apiKey = "AKIAIOSFODNN7EXAMPLE"`},
			{Kind: gitdiff.Insert, Text: "return nil"},
		}}},
	}}}
	prompt := buildPrompt(d, DefaultMaxBytes)
	if strings.Contains(prompt, "AKIAIOSFODNN7EXAMPLE") {
		t.Error("secret value must not be sent to the model")
	}
	if !strings.Contains(prompt, "[redacted possible secret]") {
		t.Error("expected the secret line to be redacted")
	}
	if !strings.Contains(prompt, "return nil") {
		t.Error("non-secret lines should still be present")
	}
}

func TestFileHunksMapping(t *testing.T) {
	in := Intent{Hunks: []HunkRef{{File: 0, Hunk: 0}, {File: 0, Hunk: 2}, {File: 1, Hunk: -1}}}
	m := FileHunks(in)
	if len(m[0]) != 2 || m[0][0] != 0 || m[0][1] != 2 {
		t.Errorf("file 0 hunks = %v", m[0])
	}
	if v, ok := m[1]; !ok || v != nil {
		t.Errorf("file 1 should map to nil (whole file), got %v ok=%v", v, ok)
	}
}
