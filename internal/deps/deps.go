// Package deps detects dependencies added or removed by a change, across the
// common ecosystems (npm, Go, pip, Cargo). It reads only the inserted and
// deleted lines of known manifest files, so it is fast, offline, and tolerant
// of anything it does not recognize.
package deps

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/agenticraptor/ghostwriter/internal/gitdiff"
)

// Change is a single dependency that was added or removed.
type Change struct {
	Ecosystem string // npm, go, pip, cargo
	Name      string
	Version   string
	Added     bool // true = added, false = removed
	File      string
}

var (
	reJSONDep = regexp.MustCompile(`^\s*"([^"]+)"\s*:\s*"([^"]+)"\s*,?\s*$`)
	reGoDep   = regexp.MustCompile(`^\s*(?:require\s+)?([a-zA-Z0-9._~-]+(?:\.[a-zA-Z]{2,})?/[^\s]+)\s+(v[0-9][^\s/]*)`)
	rePipDep  = regexp.MustCompile(`^\s*([A-Za-z0-9._-]+)\s*(?:\[[^\]]+\])?\s*(==|>=|<=|~=|!=|>|<)?\s*([0-9][\w.\-*]*)?`)
	reTOMLDep = regexp.MustCompile(`^\s*([A-Za-z0-9._-]+)\s*=\s*[{"]?\s*(?:version\s*=\s*")?([0-9^~<>=.\* ]+)?`)
)

// Analyze returns the dependency changes implied by the diff.
func Analyze(d gitdiff.Diff) []Change {
	var out []Change
	for _, f := range d.Files {
		base := strings.ToLower(filepath.Base(f.Path()))
		switch {
		case base == "package.json":
			out = append(out, scan(f, "npm", parseJSON)...)
		case base == "go.mod":
			out = append(out, scan(f, "go", parseGo)...)
		case base == "requirements.txt" || base == "pipfile":
			out = append(out, scan(f, "pip", parsePip)...)
		case base == "cargo.toml":
			out = append(out, scan(f, "cargo", parseTOML)...)
		}
	}
	return dedupe(out)
}

type parseFunc func(text string) (name, version string, ok bool)

func scan(f gitdiff.File, eco string, parse parseFunc) []Change {
	var out []Change
	for _, h := range f.Hunks {
		for _, ln := range h.Lines {
			if ln.Kind == gitdiff.Context {
				continue
			}
			name, version, ok := parse(ln.Text)
			if !ok {
				continue
			}
			out = append(out, Change{
				Ecosystem: eco,
				Name:      name,
				Version:   version,
				Added:     ln.Kind == gitdiff.Insert,
				File:      f.Path(),
			})
		}
	}
	return out
}

func parseJSON(text string) (name, version string, ok bool) {
	m := reJSONDep.FindStringSubmatch(text)
	if m == nil {
		return "", "", false
	}
	name, version = m[1], m[2]
	// Skip obvious non-dependency keys whose values are not version-like.
	if looksLikeVersion(version) || strings.HasPrefix(version, "workspace:") || strings.HasPrefix(version, "file:") {
		return name, version, true
	}
	return "", "", false
}

func parseGo(text string) (name, version string, ok bool) {
	m := reGoDep.FindStringSubmatch(text)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

func parsePip(text string) (name, version string, ok bool) {
	t := strings.TrimSpace(text)
	if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "-") {
		return "", "", false
	}
	m := rePipDep.FindStringSubmatch(t)
	if m == nil || m[1] == "" {
		return "", "", false
	}
	version = m[3]
	if m[2] != "" {
		version = m[2] + version
	}
	return m[1], version, true
}

func parseTOML(text string) (name, version string, ok bool) {
	t := strings.TrimSpace(text)
	if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "[") {
		return "", "", false
	}
	m := reTOMLDep.FindStringSubmatch(t)
	if m == nil || m[1] == "" {
		return "", "", false
	}
	return m[1], strings.TrimSpace(m[2]), true
}

func looksLikeVersion(v string) bool {
	if v == "" {
		return false
	}
	switch v[0] {
	case '^', '~', '>', '<', '=', '*':
		return true
	}
	return v[0] >= '0' && v[0] <= '9'
}

func dedupe(in []Change) []Change {
	seen := map[string]bool{}
	out := in[:0]
	for _, c := range in {
		key := c.Ecosystem + "|" + c.Name + "|" + c.Version + "|"
		if c.Added {
			key += "+"
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, c)
	}
	return out
}
