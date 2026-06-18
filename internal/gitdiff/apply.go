package gitdiff

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var (
	errBinary      = errors.New("cannot revert a binary change automatically")
	errOutsideRepo = errors.New("refusing to delete a path outside the repository")
)

// RevertResult is the outcome of reverting one file's selected hunks.
type RevertResult struct {
	Path    string
	OK      bool
	Deleted bool // true when an untracked file was removed
	Err     error
}

// Revert reverse-applies the selected hunks of the selected files back to the
// working tree.
//
// fileHunks maps an index into d.Files to the hunk indices to revert; an empty
// slice means "every hunk of that file". Untracked files are deleted outright.
// Each tracked file is first checked with `git apply --reverse --check`, so a
// file that cannot be cleanly reverted is reported as a failure and left
// completely untouched — Revert never half-applies a change.
func Revert(ctx context.Context, root string, d Diff, fileHunks map[int][]int) []RevertResult {
	indices := make([]int, 0, len(fileHunks))
	for fi := range fileHunks {
		indices = append(indices, fi)
	}
	sort.Ints(indices)

	results := make([]RevertResult, 0, len(indices))
	for _, fi := range indices {
		if fi < 0 || fi >= len(d.Files) {
			continue
		}
		f := d.Files[fi]
		res := RevertResult{Path: f.Path()}

		switch {
		case f.Untracked:
			full := filepath.Join(root, f.Path())
			if !withinRepo(root, full) {
				res.Err = errOutsideRepo
				break
			}
			// os.Remove deletes the symlink itself, not its target.
			if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
				res.Err = err
			} else {
				res.OK, res.Deleted = true, true
			}
		case f.Binary:
			res.Err = errBinary
		default:
			res.OK, res.Err = revertPatch(ctx, root, buildRevertPatch(f, fileHunks[fi]))
		}
		results = append(results, res)
	}
	return results
}

// buildRevertPatch chooses the right patch to reverse-apply. For a renamed file
// whose hunks are only partially rejected, it emits a content-only patch at the
// new path so the rename is preserved; reversing the original rename header
// would delete the destination file and lose the accepted hunks. In every other
// case the original reconstructed patch is correct (and for a whole-file reject
// of a rename, reversing the rename is exactly what we want).
func buildRevertPatch(f File, want []int) string {
	if f.Status == Renamed && isStrictHunkSubset(want, len(f.Hunks)) {
		return f.contentPatch(want...)
	}
	return f.Patch(want...)
}

// isStrictHunkSubset reports whether want selects some, but not all, of a file's
// hunks. An empty want means "the whole file", so it is never a strict subset.
func isStrictHunkSubset(want []int, total int) bool {
	if total == 0 || len(want) == 0 {
		return false
	}
	seen := make(map[int]bool, len(want))
	for _, i := range want {
		if i >= 0 && i < total {
			seen[i] = true
		}
	}
	return len(seen) < total
}

// withinRepo reports whether full resolves to a path inside root.
func withinRepo(root, full string) bool {
	rel, err := filepath.Rel(root, full)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// RejectionPatch builds a forward patch that re-creates the changes that would
// be reverted, suitable for `git apply` to restore them later. Binary changes
// cannot be represented and are skipped.
func RejectionPatch(d Diff, fileHunks map[int][]int) string {
	indices := make([]int, 0, len(fileHunks))
	for fi := range fileHunks {
		indices = append(indices, fi)
	}
	sort.Ints(indices)

	var b strings.Builder
	for _, fi := range indices {
		if fi < 0 || fi >= len(d.Files) {
			continue
		}
		f := d.Files[fi]
		switch {
		case f.Binary:
			continue
		case f.Untracked:
			b.WriteString(f.Patch()) // restoring re-creates the new file
		default:
			b.WriteString(buildRevertPatch(f, fileHunks[fi]))
		}
	}
	return b.String()
}

// WriteRejectionBackup saves a RejectionPatch under the repo's git directory so
// reverted work can be restored with `git apply <path>`. It returns the written
// path, or an empty string if there was nothing to back up.
func WriteRejectionBackup(ctx context.Context, root string, d Diff, fileHunks map[int][]int) (string, error) {
	patch := RejectionPatch(d, fileHunks)
	if strings.TrimSpace(patch) == "" {
		return "", nil
	}
	dir, err := gitDir(ctx, root)
	if err != nil {
		return "", err
	}
	outDir := filepath.Join(dir, "ghostwriter")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(outDir, fmt.Sprintf("rejected-%s.patch", time.Now().Format("20060102-150405")))
	if err := os.WriteFile(path, []byte(patch), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func revertPatch(ctx context.Context, root, patch string) (bool, error) {
	if strings.TrimSpace(patch) == "" {
		return true, nil
	}
	if err := applyReverse(ctx, root, patch, true); err != nil {
		return false, err
	}
	if err := applyReverse(ctx, root, patch, false); err != nil {
		return false, err
	}
	return true, nil
}

// applyReverse runs `git apply --reverse` (optionally just --check) with the
// patch supplied on stdin.
func applyReverse(ctx context.Context, root, patch string, check bool) error {
	args := []string{"-C", root, "apply", "--reverse", "--recount", "--whitespace=nowarn"}
	if check {
		args = append(args, "--check")
	}
	args = append(args, "-")

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Stdin = strings.NewReader(patch)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("git apply: %s", msg)
		}
		return fmt.Errorf("git apply: %w", err)
	}
	return nil
}
