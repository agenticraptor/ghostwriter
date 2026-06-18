package gitdiff

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const sampleDiff = `diff --git a/pay.go b/pay.go
index 1111111..2222222 100644
--- a/pay.go
+++ b/pay.go
@@ -10,6 +10,9 @@ func charge() error {
 	client := newClient()
-	return client.Do(req)
+	for i := 0; i < 3; i++ {
+		if err := client.Do(req); err == nil {
+			return nil
+		}
+	}
+	return errRetriesExhausted
 }
diff --git a/notes.txt b/notes.txt
deleted file mode 100644
index 3333333..0000000
--- a/notes.txt
+++ /dev/null
@@ -1,2 +0,0 @@
-old line one
-old line two
`

func TestParseFilesAndHunks(t *testing.T) {
	files := parse(sampleDiff)
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}

	pay := files[0]
	if pay.Path() != "pay.go" {
		t.Errorf("path = %q, want pay.go", pay.Path())
	}
	if pay.Status != Modified {
		t.Errorf("status = %v, want modified", pay.Status)
	}
	if len(pay.Hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(pay.Hunks))
	}
	if pay.Additions != 6 || pay.Deletions != 1 {
		t.Errorf("additions/deletions = %d/%d, want 6/1", pay.Additions, pay.Deletions)
	}
	if got := pay.Hunks[0].Section; !strings.Contains(got, "func charge()") {
		t.Errorf("section = %q, want it to mention func charge()", got)
	}

	notes := files[1]
	if notes.Status != Deleted {
		t.Errorf("notes status = %v, want deleted", notes.Status)
	}
	if notes.Path() != "notes.txt" {
		t.Errorf("notes path = %q, want notes.txt", notes.Path())
	}
}

func TestPatchRoundTrip(t *testing.T) {
	files := parse(sampleDiff)
	// Reconstructing the full patch from header + hunks must reproduce the bytes.
	got := files[0].Patch()
	if !strings.Contains(got, "@@ -10,6 +10,9 @@") {
		t.Errorf("reconstructed patch missing hunk header:\n%s", got)
	}
	if !strings.HasPrefix(got, "diff --git a/pay.go b/pay.go") {
		t.Errorf("reconstructed patch missing file header:\n%s", got)
	}
}

func TestHunkHeaderParsing(t *testing.T) {
	os, ol, ns, nl, sec := parseHunkHeader("@@ -10,6 +10,9 @@ func charge() error {")
	if os != 10 || ol != 6 || ns != 10 || nl != 9 {
		t.Errorf("got %d,%d,%d,%d want 10,6,10,9", os, ol, ns, nl)
	}
	if sec != "func charge() error {" {
		t.Errorf("section = %q", sec)
	}
	// A single-line hunk omits the count.
	_, ol2, _, nl2, _ := parseHunkHeader("@@ -5 +5,2 @@")
	if ol2 != 1 || nl2 != 2 {
		t.Errorf("got old=%d new=%d want 1,2", ol2, nl2)
	}
}

// TestCollectAndRevert exercises the full pipeline against a real repository:
// commit a baseline, modify a file and add an untracked one, collect the diff,
// then reject (revert) both and confirm the working tree returns to baseline.
func TestCollectAndRevert(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	ctx := context.Background()
	dir := t.TempDir()

	gitCmd := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(cmdEnv(), "GIT_DIR="+filepath.Join(dir, ".git"))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	gitCmd("init", "-q", "-b", "main")
	gitCmd("config", "user.email", "test@example.com")
	gitCmd("config", "user.name", "Test")
	gitCmd("config", "core.autocrlf", "false")

	write(t, dir, "hello.txt", "line one\nline two\nline three\n")
	gitCmd("add", "-A")
	gitCmd("commit", "-q", "-m", "baseline")

	// The "agent" edits a tracked file and creates a new one.
	write(t, dir, "hello.txt", "line one\nline two CHANGED\nline three\nline four\n")
	write(t, dir, "new.txt", "brand new file\n")

	diff, err := Collect(ctx, Options{Repo: dir})
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(diff.Files) != 2 {
		t.Fatalf("expected 2 changed files, got %d: %+v", len(diff.Files), paths(diff))
	}

	var untracked, tracked int = -1, -1
	for i, f := range diff.Files {
		if f.Untracked {
			untracked = i
		} else {
			tracked = i
		}
	}
	if untracked < 0 || tracked < 0 {
		t.Fatalf("expected one tracked and one untracked file, got %+v", paths(diff))
	}

	root, err := Root(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	results := Revert(ctx, root, diff, map[int][]int{
		tracked:   {}, // all hunks
		untracked: {},
	})
	for _, r := range results {
		if !r.OK {
			t.Fatalf("revert %s failed: %v", r.Path, r.Err)
		}
	}

	// hello.txt must be back to baseline; new.txt must be gone.
	if got := read(t, dir, "hello.txt"); got != "line one\nline two\nline three\n" {
		t.Errorf("hello.txt not reverted:\n%q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); !os.IsNotExist(err) {
		t.Errorf("new.txt should have been deleted, stat err = %v", err)
	}
}

// TestPartialRevertOfRenamedFile guards the safety contract: rejecting only one
// hunk of a renamed-and-modified file must revert just that hunk's content while
// keeping the rename AND the other (accepted) hunk. Reverse-applying the rename
// header would instead delete the destination file and strand the accepted work.
func TestPartialRevertOfRenamedFile(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	ctx := context.Background()
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = cmdEnv()
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	base := "l1\nl2\nALPHA-OLD\nl4\nl5\nl6\nl7\nl8\nl9\nl10\nBETA-OLD\nl12\n"
	edited := "l1\nl2\nALPHA-NEW\nl4\nl5\nl6\nl7\nl8\nl9\nl10\nBETA-NEW\nl12\n"

	git("init", "-q", "-b", "main")
	git("config", "user.email", "t@e.com")
	git("config", "user.name", "T")
	git("config", "core.autocrlf", "false")
	write(t, dir, "old.go", base)
	git("add", "-A")
	git("commit", "-q", "-m", "baseline")

	// Rename + modify two separate regions, staged so rename detection is stable.
	git("mv", "old.go", "new.go")
	write(t, dir, "new.go", edited)
	git("add", "-A")

	diff, err := Collect(ctx, Options{Repo: dir, Staged: true})
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(diff.Files) != 1 {
		t.Fatalf("expected 1 renamed file, got %d: %v", len(diff.Files), paths(diff))
	}
	f := diff.Files[0]
	if f.Status != Renamed {
		t.Fatalf("status = %v, want renamed (git did not detect the rename)", f.Status)
	}
	if len(f.Hunks) != 2 {
		t.Fatalf("expected 2 hunks (ALPHA + BETA), got %d", len(f.Hunks))
	}

	root, err := Root(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	// Reject ONLY the first hunk (the ALPHA change); keep the rename and BETA.
	results := Revert(ctx, root, diff, map[int][]int{0: {0}})
	for _, r := range results {
		if !r.OK {
			t.Fatalf("partial revert failed: %v", r.Err)
		}
	}

	// The rename must survive: new.go exists, old.go does not.
	if _, err := os.Stat(filepath.Join(dir, "new.go")); err != nil {
		t.Fatalf("new.go should still exist after partial revert: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "old.go")); !os.IsNotExist(err) {
		t.Fatalf("old.go must not reappear (rename must be preserved), stat err = %v", err)
	}
	got := read(t, dir, "new.go")
	if !strings.Contains(got, "ALPHA-OLD") {
		t.Errorf("ALPHA hunk should have been reverted to ALPHA-OLD:\n%s", got)
	}
	if !strings.Contains(got, "BETA-NEW") {
		t.Errorf("BETA hunk was accepted and must be kept as BETA-NEW:\n%s", got)
	}
}

func cmdEnv() []string {
	return append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func paths(d Diff) []string {
	var out []string
	for _, f := range d.Files {
		out = append(out, f.Path())
	}
	return out
}
