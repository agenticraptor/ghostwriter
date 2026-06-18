// Package gitdiff collects and parses the changes an AI agent (or anyone) has
// made to a git repository, and can reverse-apply a selected subset of those
// changes back to the working tree.
//
// It shells out to the system `git` binary rather than reimplementing diffing,
// then parses the unified-diff output into structured files and hunks while
// preserving the exact patch bytes so any subset can be reconstructed and
// reverted with `git apply --reverse`.
package gitdiff

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Status describes how a file changed.
type Status int

// File statuses.
const (
	Modified Status = iota
	Added
	Deleted
	Renamed
)

func (s Status) String() string {
	switch s {
	case Added:
		return "added"
	case Deleted:
		return "deleted"
	case Renamed:
		return "renamed"
	default:
		return "modified"
	}
}

// LineKind is the kind of a single diff line.
type LineKind byte

// Line kinds.
const (
	Context LineKind = ' '
	Insert  LineKind = '+'
	Delete  LineKind = '-'
)

// Line is a single line within a hunk, with its leading marker stripped.
type Line struct {
	Kind LineKind
	Text string
}

// Hunk is a contiguous change region within a file.
type Hunk struct {
	OldStart  int
	OldLines  int
	NewStart  int
	NewLines  int
	Section   string // function/context text after the second @@
	Lines     []Line
	Raw       string // exact patch bytes for this hunk (the @@ line + body)
	Additions int
	Deletions int
}

// File is the set of changes to a single path.
type File struct {
	OldPath   string
	NewPath   string
	Status    Status
	Binary    bool
	Untracked bool   // synthesized from an untracked working-tree file
	Header    string // exact patch header bytes ("diff --git" through "+++")
	Hunks     []Hunk
	Additions int
	Deletions int
}

// Path returns the path to display for the file.
func (f File) Path() string {
	if f.Status == Deleted && f.OldPath != "" {
		return f.OldPath
	}
	if f.NewPath != "" {
		return f.NewPath
	}
	return f.OldPath
}

// Patch reconstructs a valid patch for the file containing only the hunks whose
// indices are listed in want. With no indices it returns the full-file patch.
func (f File) Patch(want ...int) string {
	var b strings.Builder
	b.WriteString(f.Header)
	if len(want) == 0 {
		for _, h := range f.Hunks {
			b.WriteString(h.Raw)
		}
		return b.String()
	}
	for _, i := range want {
		if i >= 0 && i < len(f.Hunks) {
			b.WriteString(f.Hunks[i].Raw)
		}
	}
	return b.String()
}

// contentPatch builds a content-only "modify" patch at the file's new path,
// dropping any rename / new-file / delete header lines. It is used to revert a
// subset of a renamed file's hunks WITHOUT also reversing the rename itself:
// reverse-applying the original rename header would delete the destination file
// and strand the hunks the reviewer accepted.
func (f File) contentPatch(want ...int) string {
	path := f.NewPath
	if path == "" {
		path = f.OldPath
	}
	var b strings.Builder
	b.WriteString("diff --git a/" + path + " b/" + path + "\n")
	b.WriteString("--- a/" + path + "\n")
	b.WriteString("+++ b/" + path + "\n")
	if len(want) == 0 {
		for _, h := range f.Hunks {
			b.WriteString(h.Raw)
		}
		return b.String()
	}
	for _, i := range want {
		if i >= 0 && i < len(f.Hunks) {
			b.WriteString(f.Hunks[i].Raw)
		}
	}
	return b.String()
}

// Diff is the complete set of changed files.
type Diff struct {
	Files []File
}

// Empty reports whether the diff contains no changes.
func (d Diff) Empty() bool { return len(d.Files) == 0 }

// Additions returns the total inserted lines across all files.
func (d Diff) Additions() int {
	n := 0
	for _, f := range d.Files {
		n += f.Additions
	}
	return n
}

// Deletions returns the total removed lines across all files.
func (d Diff) Deletions() int {
	n := 0
	for _, f := range d.Files {
		n += f.Deletions
	}
	return n
}

// Options controls what changes Collect gathers.
type Options struct {
	// Repo is any path inside the target repository (default ".").
	Repo string
	// Against is the ref to compare the working tree against (default "HEAD").
	Against string
	// Staged compares the index against Against instead of the working tree.
	Staged bool
	// ExcludeUntracked turns off the default behavior of synthesizing diffs for
	// new, untracked files. Untracked files are never included in Staged mode.
	ExcludeUntracked bool
	// MaxFileBytes caps the size of an untracked file that will be inlined.
	MaxFileBytes int
}

func (o Options) repo() string {
	if strings.TrimSpace(o.Repo) == "" {
		return "."
	}
	return o.Repo
}

func (o Options) against() string {
	if strings.TrimSpace(o.Against) == "" {
		return "HEAD"
	}
	return o.Against
}

func (o Options) maxFileBytes() int {
	if o.MaxFileBytes <= 0 {
		return 256 << 10 // 256 KiB
	}
	return o.MaxFileBytes
}

// emptyTree is git's well-known hash of the empty tree object. Diffing against
// it makes every tracked/staged file appear as an addition, which is the sane
// behavior for a repository that has no commits yet.
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// Root returns the absolute path to the repository root containing repo.
func Root(ctx context.Context, repo string) (string, error) {
	if strings.TrimSpace(repo) == "" {
		repo = "."
	}
	out, err := run(ctx, repo, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("not a git repository (%s): %w", repo, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// gitDir returns the absolute path to the repository's git directory (which may
// live outside the work tree for worktrees and submodules).
func gitDir(ctx context.Context, root string) (string, error) {
	out, err := run(ctx, root, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// resolveBase validates the comparison ref and substitutes the empty tree when
// the default HEAD does not yet exist. It rejects refs beginning with '-' so a
// value like "--output=/etc/passwd" can never be smuggled in as a git option.
func resolveBase(ctx context.Context, root, against string) (string, error) {
	against = strings.TrimSpace(against)
	if against == "" {
		against = "HEAD"
	}
	if strings.HasPrefix(against, "-") {
		return "", fmt.Errorf("invalid ref %q: must not start with '-'", against)
	}
	if against == "HEAD" {
		if _, err := run(ctx, root, "rev-parse", "--verify", "--quiet", "HEAD"); err != nil {
			return emptyTree, nil // a repo with no commits yet
		}
	}
	return against, nil
}

// Collect gathers the changes described by opts into a Diff.
func Collect(ctx context.Context, opts Options) (Diff, error) {
	root, err := Root(ctx, opts.repo())
	if err != nil {
		return Diff{}, err
	}

	base, err := resolveBase(ctx, root, opts.against())
	if err != nil {
		return Diff{}, err
	}

	args := []string{
		"-c", "core.quotepath=false",
		"diff", "--no-color", "--no-ext-diff", "-M",
	}
	if opts.Staged {
		args = append(args, "--cached")
	}
	// The trailing "--" terminates options/revisions so a ref can never be
	// interpreted as a pathspec, and the validated base can never be an option.
	args = append(args, base, "--")

	out, err := run(ctx, root, args...)
	if err != nil {
		return Diff{}, fmt.Errorf("git diff: %w", err)
	}
	files := parse(string(out))

	includeUntracked := !opts.ExcludeUntracked && !opts.Staged
	if includeUntracked {
		extra, err := collectUntracked(ctx, root, opts.maxFileBytes())
		if err == nil {
			files = append(files, extra...)
		}
	}

	return Diff{Files: files}, nil
}

// collectUntracked synthesizes an Added File for every untracked, non-ignored
// file in the working tree (skipping binary or over-large files).
func collectUntracked(ctx context.Context, root string, maxBytes int) ([]File, error) {
	out, err := run(ctx, root, "-c", "core.quotepath=false",
		"ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	var files []File
	for _, rel := range strings.Split(string(out), "\x00") {
		rel = strings.TrimSpace(rel)
		if rel == "" {
			continue
		}
		full := filepath.Join(root, rel)
		// Lstat (not Stat) so we never follow a symlink. An untracked symlink
		// could point anywhere on disk (e.g. /etc/passwd); reading it would leak
		// that file into the review and, with a cloud model, off the machine.
		info, err := os.Lstat(full)
		if err != nil || info.IsDir() {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			files = append(files, newUntrackedSymlink(rel))
			continue
		}
		if !info.Mode().IsRegular() {
			continue // skip fifos, sockets, devices, etc.
		}
		if info.Size() > int64(maxBytes) {
			files = append(files, newUntrackedPlaceholder(rel, true))
			continue
		}
		data, err := os.ReadFile(full) //nolint:gosec // regular file rooted at the repo, size-capped above
		if err != nil {
			continue
		}
		if bytes.IndexByte(data, 0) >= 0 {
			files = append(files, newUntrackedPlaceholder(rel, false))
			continue
		}
		files = append(files, newUntrackedFile(rel, string(data)))
	}
	return files, nil
}

// run executes git inside dir and returns stdout, or an error including stderr.
func run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return nil, fmt.Errorf("%w: %s", err, msg)
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}
