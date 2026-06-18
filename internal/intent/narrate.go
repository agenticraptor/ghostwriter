package intent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/agenticraptor/ghostwriter/internal/gitdiff"
	"github.com/agenticraptor/ghostwriter/internal/llm"
	"github.com/agenticraptor/ghostwriter/internal/risk"
)

// DefaultMaxBytes caps how much diff text is sent to the model.
const DefaultMaxBytes = 14000

const systemPrompt = `You are ghostwriter, a meticulous senior engineer reviewing the changes an AI coding agent just made to a repository.

Your job: group the changed hunks into a SMALL number of INTENTS. An intent is one logical thing the agent was trying to accomplish (e.g. "Add retry logic to the payments client", "Update tests for the new behavior", "Bump the Stripe SDK"). Related hunks across different files belong to the same intent.

Rules:
- Be concrete and skeptical. Prefer the agent's real goal over a vague restatement of the diff.
- Each hunk id must appear in at most one intent. Cover every hunk you can.
- Keep "title" under 8 words. Keep "summary" to one plain-English sentence a busy reviewer can skim. Use "why" only if the motivation is non-obvious.
- "category" must be one of: feature, fix, refactor, test, docs, deps, config, chore, other.

Respond with ONLY a JSON object, no prose, in exactly this shape:
{"intents":[{"title":"...","summary":"...","why":"...","category":"feature","hunks":["0:0","1:0"]}]}`

type aiResponse struct {
	Intents []aiIntent `json:"intents"`
}

type aiIntent struct {
	Title    string   `json:"title"`
	Summary  string   `json:"summary"`
	Why      string   `json:"why"`
	Category string   `json:"category"`
	Hunks    []string `json:"hunks"`
}

// Narrate asks the model to group the diff into intents. It guarantees every
// hunk is accounted for: anything the model omits is collected into a final
// "Other changes" intent. The returned intents are fully annotated.
func Narrate(ctx context.Context, client llm.Client, d gitdiff.Diff, maxBytes int) ([]Intent, error) {
	if client == nil {
		return nil, errors.New("no model client")
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	prompt := buildPrompt(d, maxBytes)

	raw, err := client.Complete(ctx, llm.Request{
		System:      systemPrompt,
		Prompt:      prompt,
		MaxTokens:   2048,
		Temperature: 0.1,
	})
	if err != nil {
		return nil, err
	}

	resp, err := parseResponse(raw)
	if err != nil {
		return nil, err
	}

	assigned := map[string]bool{}
	var intents []Intent
	for _, ai := range resp.Intents {
		var refs []HunkRef
		for _, id := range ai.Hunks {
			ref, ok := parseRef(id, d)
			if !ok {
				continue
			}
			key := fmt.Sprintf("%d:%d", ref.File, ref.Hunk)
			if assigned[key] {
				continue
			}
			assigned[key] = true
			refs = append(refs, ref)
		}
		if len(refs) == 0 && strings.TrimSpace(ai.Title) == "" {
			continue
		}
		intents = append(intents, Intent{
			Title:    strings.TrimSpace(ai.Title),
			Summary:  strings.TrimSpace(ai.Summary),
			Why:      strings.TrimSpace(ai.Why),
			Category: Category(ai.Category).normalize(),
			Hunks:    refs,
		})
	}

	if leftover := unassigned(d, assigned); len(leftover) > 0 {
		intents = append(intents, Intent{
			Title:    "Other changes",
			Summary:  "Remaining changes the narrator did not group.",
			Category: Other,
			Hunks:    leftover,
		})
	}

	if len(intents) == 0 {
		return nil, errors.New("model returned no usable intents")
	}
	Annotate(intents, d)
	return intents, nil
}

// buildPrompt renders the diff as an indexed, size-capped listing.
func buildPrompt(d gitdiff.Diff, maxBytes int) string {
	var b strings.Builder
	b.WriteString("Here is the diff to review, with stable file (F) and hunk (H) indices.\n")
	b.WriteString("Reference hunks as \"F:H\" (for example \"0:1\").\n\n")

	budget := maxBytes
	for fi, f := range d.Files {
		fmt.Fprintf(&b, "### F%d  %s  %s  (+%d/-%d)\n", fi, f.Status, f.Path(), f.Additions, f.Deletions)
		if f.Binary {
			b.WriteString("    (binary or non-text change)\n")
			continue
		}
		for hi, h := range f.Hunks {
			fmt.Fprintf(&b, "  H%d  @@ %s\n", hi, strings.TrimSpace(h.Section))
			if budget <= 0 {
				b.WriteString("    …(diff truncated)\n")
				continue
			}
			for _, ln := range h.Lines {
				if ln.Kind == gitdiff.Context {
					continue
				}
				text := clip(ln.Text, 200)
				if risk.LooksSecret(ln.Text) {
					text = "[redacted possible secret]"
				}
				line := fmt.Sprintf("    %c %s\n", ln.Kind, text)
				if budget-len(line) <= 0 {
					b.WriteString("    …(diff truncated)\n")
					budget = 0
					break
				}
				b.WriteString(line)
				budget -= len(line)
			}
		}
	}
	return b.String()
}

func parseResponse(raw string) (aiResponse, error) {
	js := extractJSON(raw)
	if js == "" {
		return aiResponse{}, errors.New("no JSON object in model response")
	}
	var resp aiResponse
	if err := json.Unmarshal([]byte(js), &resp); err != nil {
		return aiResponse{}, fmt.Errorf("parse model JSON: %w", err)
	}
	return resp, nil
}

// extractJSON pulls the first balanced top-level JSON object out of s, tolerating
// markdown fences and surrounding prose.
func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "```"); i >= 0 {
		rest := s[i+3:]
		rest = strings.TrimPrefix(rest, "json")
		if j := strings.Index(rest, "```"); j >= 0 {
			s = strings.TrimSpace(rest[:j])
		}
	}
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return ""
	}
	depth, inStr, esc := 0, false, false
	for i := start; i < len(s); i++ {
		c := s[i]
		switch {
		case esc:
			esc = false
		case c == '\\' && inStr:
			esc = true
		case c == '"':
			inStr = !inStr
		case inStr:
			// skip
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

// parseRef parses "F:H", "0:1", or a bare hunk index (when there is one file).
func parseRef(id string, d gitdiff.Diff) (HunkRef, bool) {
	id = strings.TrimSpace(id)
	id = strings.ReplaceAll(id, "F", "")
	id = strings.ReplaceAll(id, "H", "")
	id = strings.ReplaceAll(id, "f", "")
	id = strings.ReplaceAll(id, "h", "")
	parts := strings.Split(id, ":")
	var fi, hi int
	var err error
	switch len(parts) {
	case 1:
		if len(d.Files) != 1 {
			return HunkRef{}, false
		}
		hi, err = strconv.Atoi(strings.TrimSpace(parts[0]))
	case 2:
		fi, err = strconv.Atoi(strings.TrimSpace(parts[0]))
		if err == nil {
			hi, err = strconv.Atoi(strings.TrimSpace(parts[1]))
		}
	default:
		return HunkRef{}, false
	}
	if err != nil || fi < 0 || fi >= len(d.Files) {
		return HunkRef{}, false
	}
	if hi < 0 || hi >= len(d.Files[fi].Hunks) {
		return HunkRef{}, false
	}
	return HunkRef{File: fi, Hunk: hi}, true
}

// unassigned returns refs for every hunk not already claimed by an intent.
func unassigned(d gitdiff.Diff, assigned map[string]bool) []HunkRef {
	var refs []HunkRef
	for fi, f := range d.Files {
		if len(f.Hunks) == 0 {
			key := fmt.Sprintf("%d:-1", fi)
			if !assigned[key] {
				assigned[key] = true
				refs = append(refs, HunkRef{File: fi, Hunk: -1})
			}
			continue
		}
		for hi := range f.Hunks {
			key := fmt.Sprintf("%d:%d", fi, hi)
			if !assigned[key] {
				refs = append(refs, HunkRef{File: fi, Hunk: hi})
			}
		}
	}
	return refs
}

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
