package gitdiff

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func newRepo(t *testing.T) (string, func(args ...string)) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = cmdEnv()
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	git("init", "-q", "-b", "main")
	git("config", "user.email", "t@e.com")
	git("config", "user.name", "T")
	git("config", "core.autocrlf", "false") // deterministic line endings on Windows CI
	return dir, git
}

// TestCollectNoHEAD: a repository with no commits yet must produce a clean
// review (everything as additions) rather than a cryptic "ambiguous HEAD" error.
func TestCollectNoHEAD(t *testing.T) {
	dir, git := newRepo(t)
	write(t, dir, "main.go", "package main\n\nfunc main() {}\n")
	git("add", "-A") // staged, but never committed -> no HEAD

	diff, err := Collect(context.Background(), Options{Repo: dir})
	if err != nil {
		t.Fatalf("collect on a repo with no commits should succeed, got: %v", err)
	}
	if diff.Empty() {
		t.Fatal("expected the new file to appear as a change")
	}
	if diff.Files[0].Status != Added {
		t.Errorf("file status = %v, want added", diff.Files[0].Status)
	}
}

// TestUntrackedSymlinkNotFollowed: an untracked symlink must never have its
// target read into the diff (which would leak arbitrary files, including to a
// cloud model).
func TestUntrackedSymlinkNotFollowed(t *testing.T) {
	dir, git := newRepo(t)
	write(t, dir, "real.txt", "real\n")
	git("add", "-A")
	git("commit", "-q", "-m", "base")

	// A secret file outside the repo, and an untracked symlink pointing at it.
	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("TOP-SECRET-PASSWORD\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(dir, "link.txt")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	diff, err := Collect(context.Background(), Options{Repo: dir})
	if err != nil {
		t.Fatal(err)
	}
	var link *File
	for i := range diff.Files {
		if diff.Files[i].Path() == "link.txt" {
			link = &diff.Files[i]
		}
	}
	if link == nil {
		t.Fatal("symlink should still appear in the review")
	}
	if len(link.Hunks) != 0 || !link.Binary {
		t.Errorf("symlink should be a contentless placeholder, got %d hunks binary=%v", len(link.Hunks), link.Binary)
	}
	// The secret target must not appear anywhere in the collected diff.
	for _, f := range diff.Files {
		for _, h := range f.Hunks {
			if strings.Contains(h.Raw, "TOP-SECRET") {
				t.Fatal("symlink target content leaked into the diff")
			}
		}
	}
}

// TestResolveBaseRejectsOptionLikeRef: a ref starting with '-' must be rejected
// so it can never be smuggled to git as an option (e.g. --output=/path).
func TestResolveBaseRejectsOptionLikeRef(t *testing.T) {
	dir, git := newRepo(t)
	write(t, dir, "x", "1\n")
	git("add", "-A")
	git("commit", "-q", "-m", "base")

	_, err := Collect(context.Background(), Options{Repo: dir, Against: "--output=/tmp/should_not_exist"})
	if err == nil {
		t.Fatal("expected an error for an option-like ref")
	}
	if !strings.Contains(err.Error(), "must not start with") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestRejectionBackupRestores: the backup patch written before a revert must
// faithfully restore the reverted change via `git apply`.
func TestRejectionBackupRestores(t *testing.T) {
	dir, git := newRepo(t)
	write(t, dir, "f.txt", "one\ntwo\nthree\n")
	git("add", "-A")
	git("commit", "-q", "-m", "base")
	write(t, dir, "f.txt", "one\nTWO-CHANGED\nthree\n")

	ctx := context.Background()
	diff, err := Collect(ctx, Options{Repo: dir})
	if err != nil {
		t.Fatal(err)
	}
	root, _ := Root(ctx, dir)

	backup, err := WriteRejectionBackup(ctx, root, diff, map[int][]int{0: {}})
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	if backup == "" {
		t.Fatal("expected a backup path")
	}

	// Revert, then confirm the change is gone.
	for _, r := range Revert(ctx, root, diff, map[int][]int{0: {}}) {
		if !r.OK {
			t.Fatalf("revert failed: %v", r.Err)
		}
	}
	if got := read(t, dir, "f.txt"); strings.Contains(got, "TWO-CHANGED") {
		t.Fatal("change should have been reverted")
	}

	// Restoring from the backup must bring the change back.
	cmd := exec.Command("git", "-C", dir, "apply", backup)
	cmd.Env = cmdEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("restore from backup failed: %v\n%s", err, out)
	}
	if got := read(t, dir, "f.txt"); !strings.Contains(got, "TWO-CHANGED") {
		t.Errorf("backup did not restore the change:\n%s", got)
	}
}
