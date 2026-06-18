package gitdiff

import (
	"strconv"
	"strings"
)

// parse turns raw `git diff` output into a slice of Files, preserving the exact
// patch bytes for each file header and hunk so they can be reconstructed later.
func parse(out string) []File {
	if strings.TrimSpace(out) == "" {
		return nil
	}
	lines := strings.SplitAfter(out, "\n") // keep trailing "\n" on each element

	var files []File
	i := 0
	for i < len(lines) {
		if !strings.HasPrefix(lines[i], "diff --git ") {
			i++
			continue
		}
		var f File
		// --- Header: accumulate until the first hunk / binary marker / next file.
		var header strings.Builder
		header.WriteString(lines[i])
		i++
		for i < len(lines) {
			ln := lines[i]
			if strings.HasPrefix(ln, "@@ ") || strings.HasPrefix(ln, "diff --git ") {
				break
			}
			switch {
			case strings.HasPrefix(ln, "new file mode"):
				f.Status = Added
			case strings.HasPrefix(ln, "deleted file mode"):
				f.Status = Deleted
			case strings.HasPrefix(ln, "rename from "):
				f.OldPath = strings.TrimRight(trimPrefix(ln, "rename from "), "\n")
				f.Status = Renamed
			case strings.HasPrefix(ln, "rename to "):
				f.NewPath = strings.TrimRight(trimPrefix(ln, "rename to "), "\n")
				f.Status = Renamed
			case strings.HasPrefix(ln, "--- "):
				f.OldPath = pathFromHeader(ln, f.OldPath)
			case strings.HasPrefix(ln, "+++ "):
				f.NewPath = pathFromHeader(ln, f.NewPath)
			case strings.HasPrefix(ln, "Binary files "):
				f.Binary = true
			}
			header.WriteString(ln)
			i++
		}
		f.Header = header.String()
		if f.OldPath == "" && f.NewPath == "" {
			f.OldPath, f.NewPath = pathsFromGitLine(f.Header)
		}
		f.classify()

		// --- Hunks.
		for i < len(lines) && strings.HasPrefix(lines[i], "@@ ") {
			h, next := parseHunk(lines, i)
			f.Hunks = append(f.Hunks, h)
			f.Additions += h.Additions
			f.Deletions += h.Deletions
			i = next
		}
		files = append(files, f)
	}
	return files
}

// parseHunk reads one hunk starting at lines[start] (the "@@" line) and returns
// it plus the index of the next unconsumed line.
func parseHunk(lines []string, start int) (Hunk, int) {
	h := Hunk{}
	head := lines[start]
	h.OldStart, h.OldLines, h.NewStart, h.NewLines, h.Section = parseHunkHeader(head)

	var raw strings.Builder
	raw.WriteString(head)

	i := start + 1
	for i < len(lines) {
		ln := lines[i]
		if ln == "" { // trailing empty element from SplitAfter
			i++
			continue
		}
		c := ln[0]
		if c != '+' && c != '-' && c != ' ' && c != '\\' {
			// End of this hunk: a new hunk/file header, or an unrecognized line.
			// Stop here WITHOUT consuming the line so the outer parser can
			// resynchronize at the next "diff --git"/"@@" instead of dropping
			// every remaining file.
			break
		}
		raw.WriteString(ln)
		switch c {
		case '+':
			h.Lines = append(h.Lines, Line{Kind: Insert, Text: body(ln)})
			h.Additions++
		case '-':
			h.Lines = append(h.Lines, Line{Kind: Delete, Text: body(ln)})
			h.Deletions++
		case ' ':
			h.Lines = append(h.Lines, Line{Kind: Context, Text: body(ln)})
		case '\\':
			// "\ No newline at end of file" — kept in Raw, not shown as a line.
		}
		i++
	}
	h.Raw = raw.String()
	return h, i
}

// parseHunkHeader parses "@@ -oldStart,oldLines +newStart,newLines @@ section".
func parseHunkHeader(s string) (oldStart, oldLines, newStart, newLines int, section string) {
	oldLines, newLines = 1, 1
	s = strings.TrimRight(s, "\n")
	rest := s
	if idx := strings.Index(rest, "@@ "); idx == 0 {
		rest = rest[3:]
	}
	// rest now looks like "-a,b +c,d @@ section"
	end := strings.Index(rest, " @@")
	spec := rest
	if end >= 0 {
		spec = rest[:end]
		section = strings.TrimPrefix(rest[end+3:], " ")
	}
	for _, tok := range strings.Fields(spec) {
		switch {
		case strings.HasPrefix(tok, "-"):
			oldStart, oldLines = parseRange(tok[1:])
		case strings.HasPrefix(tok, "+"):
			newStart, newLines = parseRange(tok[1:])
		}
	}
	return oldStart, oldLines, newStart, newLines, section
}

// parseRange parses "start" or "start,count".
func parseRange(s string) (start, count int) {
	count = 1
	if comma := strings.IndexByte(s, ','); comma >= 0 {
		start = atoi(s[:comma])
		count = atoi(s[comma+1:])
	} else {
		start = atoi(s)
	}
	return start, count
}

func (f *File) classify() {
	if f.Status == Added || f.Status == Deleted || f.Status == Renamed {
		return
	}
	switch {
	case f.OldPath == "" && f.NewPath != "":
		f.Status = Added
	case f.OldPath != "" && f.NewPath == "":
		f.Status = Deleted
	case f.OldPath != "" && f.NewPath != "" && f.OldPath != f.NewPath:
		f.Status = Renamed
	default:
		f.Status = Modified
	}
}

// pathFromHeader extracts the path from a "--- a/path" or "+++ b/path" line.
func pathFromHeader(ln, fallback string) string {
	p := strings.TrimRight(ln[4:], "\n")
	p = strings.TrimSpace(p)
	if p == "/dev/null" {
		return ""
	}
	p = strings.TrimPrefix(p, "a/")
	p = strings.TrimPrefix(p, "b/")
	if p == "" {
		return fallback
	}
	return p
}

// pathsFromGitLine parses "diff --git a/old b/new" as a last resort.
func pathsFromGitLine(header string) (oldPath, newPath string) {
	first := header
	if nl := strings.IndexByte(header, '\n'); nl >= 0 {
		first = header[:nl]
	}
	first = strings.TrimPrefix(first, "diff --git ")
	fields := strings.Fields(first)
	if len(fields) == 2 {
		return strings.TrimPrefix(fields[0], "a/"), strings.TrimPrefix(fields[1], "b/")
	}
	return "", ""
}

// newUntrackedFile synthesizes an Added File for an untracked working-tree file.
func newUntrackedFile(rel, content string) File {
	f := File{
		NewPath:   rel,
		Status:    Added,
		Untracked: true,
		Header: "diff --git a/" + rel + " b/" + rel + "\n" +
			"new file mode 100644\n" +
			"--- /dev/null\n" +
			"+++ b/" + rel + "\n",
	}
	if content == "" {
		return f
	}
	parts := strings.Split(content, "\n")
	trailingNewline := strings.HasSuffix(content, "\n")
	if trailingNewline {
		parts = parts[:len(parts)-1] // drop the empty element after the final "\n"
	}
	var raw strings.Builder
	raw.WriteString("@@ -0,0 +1," + strconv.Itoa(len(parts)) + " @@\n")
	h := Hunk{OldStart: 0, OldLines: 0, NewStart: 1, NewLines: len(parts)}
	for idx, p := range parts {
		raw.WriteString("+" + p + "\n")
		h.Lines = append(h.Lines, Line{Kind: Insert, Text: p})
		h.Additions++
		if idx == len(parts)-1 && !trailingNewline {
			raw.WriteString("\\ No newline at end of file\n")
		}
	}
	h.Raw = raw.String()
	f.Hunks = []Hunk{h}
	f.Additions = h.Additions
	return f
}

// newUntrackedSymlink represents an untracked symbolic link. Its target is
// never read or shown; the link itself can still be rejected (removed).
func newUntrackedSymlink(rel string) File {
	return File{
		NewPath:   rel,
		Status:    Added,
		Untracked: true,
		Binary:    true,
		Header: "diff --git a/" + rel + " b/" + rel + "\n" +
			"new file mode 120000\n" +
			"Symbolic link /dev/null and b/" + rel + " (target not read)\n",
	}
}

// newUntrackedPlaceholder represents an untracked file we cannot inline (binary
// or too large). It still appears in the review and can be rejected (deleted).
func newUntrackedPlaceholder(rel string, tooLarge bool) File {
	note := "Binary files /dev/null and b/" + rel + " differ\n"
	if tooLarge {
		note = "Binary files /dev/null and b/" + rel + " differ (file too large to inline)\n"
	}
	return File{
		NewPath:   rel,
		Status:    Added,
		Untracked: true,
		Binary:    true,
		Header: "diff --git a/" + rel + " b/" + rel + "\n" +
			"new file mode 100644\n" + note,
	}
}

func body(ln string) string {
	if len(ln) == 0 {
		return ""
	}
	return strings.TrimRight(ln[1:], "\n")
}

func trimPrefix(s, prefix string) string { return strings.TrimPrefix(s, prefix) }

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}
