// Created by nc.ai 0.8.9.7 on new-admin-pc 2026-10-28 09:38:58
// Package history searches the chat transcripts of one project.
//
// Why: the answer to "we already discussed this" usually lives in an earlier
// chat, not in the code. Without search, the agent either re-derives it or the
// user retypes the context. A hit is a short snippet plus the chat title, so
// the model can decide in one step whether it is worth reading further.
package history

import (
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"notcursor.ai/app/internal/chatstore"
	"notcursor.ai/app/internal/llm"
)

const (
	// snippetRadius is how much text is kept around the first match.
	snippetRadius = 180
	// perSession caps hits from one chat so a single long chat cannot flood.
	perSession = 2
	// minScore drops the noise of one weak token inside a huge message.
	minScore = 16
)

// Hit is one matched message fragment.
type Hit struct {
	SessionID string
	Title     string
	Updated   time.Time
	Role      string
	Snippet   string
	Score     int
	Archived  bool
}

// Searcher searches the chats of one workspace. A nil store yields no hits.
type Searcher struct {
	store *chatstore.Store
}

// New returns a searcher over store.
func New(store *chatstore.Store) *Searcher {
	return &Searcher{store: store}
}

// Search ranks messages of project against query and returns up to limit hits.
func (s *Searcher) Search(project, query string, limit int) []Hit {
	if s == nil || s.store == nil || strings.TrimSpace(project) == "" {
		return nil
	}
	tokens := tokenize(query)
	phrase := strings.ToLower(strings.TrimSpace(query))
	if len(tokens) == 0 && phrase == "" {
		return nil
	}
	if limit <= 0 {
		limit = 8
	}
	if limit > 50 {
		limit = 50
	}

	var hits []Hit
	if b, err := s.store.List(project); err == nil && b != nil {
		// Newest chats first: an older chat is a worse answer at equal score.
		sessions := append([]chatstore.Session(nil), b.Sessions...)
		sort.SliceStable(sessions, func(i, j int) bool {
			return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
		})
		for _, sess := range sessions {
			hits = append(hits, scoreSession(sess.ID, sess.Title, sess.UpdatedAt, false, sess.History, tokens, phrase)...)
		}
	}
	if archived, err := s.store.ListArchived(); err == nil {
		for _, chat := range archived {
			if strings.TrimSpace(chat.Project) != strings.TrimSpace(project) {
				continue
			}
			hits = append(hits, scoreSession(chat.ID, chat.Title, chat.ArchivedAt, true, chat.History, tokens, phrase)...)
		}
	}

	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].Updated.After(hits[j].Updated)
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

// scoreSession returns the best perSession hits of one chat.
func scoreSession(sessionID, title string, updated time.Time, archived bool, msgs []llm.Message, tokens []string, phrase string) []Hit {
	titleLower := strings.ToLower(title)
	titleScore := 0
	for _, t := range tokens {
		if strings.Contains(titleLower, t) {
			titleScore += 25
		}
	}

	best := make([]Hit, 0, perSession)
	for _, m := range msgs {
		text := strings.TrimSpace(m.Content)
		if text == "" || len(text) < 8 {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(m.Role))
		if role != "user" && role != "assistant" {
			continue
		}
		content := strings.ToLower(text)
		sc := 0
		for _, t := range tokens {
			n := strings.Count(content, t)
			if n == 0 {
				continue
			}
			if n > 3 {
				n = 3
			}
			sc += 8 * n
		}
		if phrase != "" && strings.Contains(content, phrase) {
			sc += 40
		}
		if sc == 0 {
			continue
		}
		if role == "user" {
			// The question frames the answer; a user line is usually the better entry point.
			sc += 4
		}
		sc += titleScore
		if sc < minScore {
			continue
		}
		hit := Hit{
			SessionID: sessionID,
			Title:     strings.TrimSpace(title),
			Updated:   updated,
			Role:      role,
			Snippet:   snippet(text, tokens, phrase),
			Score:     sc,
			Archived:  archived,
		}
		best = append(best, hit)
		sort.SliceStable(best, func(i, j int) bool { return best[i].Score > best[j].Score })
		if len(best) > perSession {
			best = best[:perSession]
		}
	}
	return best
}

// snippet returns one line of text centred on the first match.
func snippet(text string, tokens []string, phrase string) string {
	lower := strings.ToLower(text)
	pos := -1
	if phrase != "" {
		pos = strings.Index(lower, phrase)
	}
	if pos < 0 {
		for _, t := range tokens {
			if i := strings.Index(lower, t); i >= 0 && (pos < 0 || i < pos) {
				pos = i
			}
		}
	}
	if pos < 0 {
		pos = 0
	}
	start := pos - snippetRadius
	if start < 0 {
		start = 0
	}
	end := pos + snippetRadius
	if end > len(text) {
		end = len(text)
	}
	// Never cut a UTF-8 rune in half.
	for start > 0 && !utf8.RuneStart(text[start]) {
		start--
	}
	for end < len(text) && !utf8.RuneStart(text[end]) {
		end++
	}
	out := strings.Join(strings.Fields(text[start:end]), " ")
	if start > 0 {
		out = "…" + out
	}
	if end < len(text) {
		out += "…"
	}
	return out
}

// tokenize splits a query into lowercase words suitable for substring match.
func tokenize(q string) []string {
	fields := strings.FieldsFunc(strings.ToLower(q), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' ||
			r >= 'а' && r <= 'я' || r == 'ё' || r == '_' || r == '-' || r == '.')
	})
	out := make([]string, 0, len(fields))
	seen := map[string]bool{}
	for _, f := range fields {
		f = strings.Trim(f, "._-")
		if len(f) < 3 || seen[f] {
			continue
		}
		seen[f] = true
		out = append(out, f)
	}
	return out
}

// Format renders hits as compact one-block entries for a tool result.
func Format(hits []Hit) string {
	if len(hits) == 0 {
		return ""
	}
	var b strings.Builder
	for _, h := range hits {
		title := h.Title
		if title == "" {
			title = "(без названия)"
		}
		b.WriteString("· ")
		if !h.Updated.IsZero() {
			b.WriteString(h.Updated.Local().Format("2006-01-02"))
			b.WriteString(" ")
		}
		b.WriteString("«" + title + "»")
		if h.Archived {
			b.WriteString(" [архив]")
		}
		b.WriteString(" · " + h.Role + " · " + h.SessionID)
		b.WriteString("\n  " + h.Snippet + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
