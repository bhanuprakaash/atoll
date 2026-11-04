package memory

import (
	"context"
	"slices"
	"strings"
	"unicode"
)

const (
	_defaultLimit = 10
	// _maxWords caps the queries one Relevant call runs, so a pasted log stays cheap.
	_maxWords = 64
)

var (
	_stopWords = map[string]bool{
		"the": true, "a": true, "an": true, "and": true, "or": true, "for": true, "of": true,
		"to": true, "in": true, "on": true, "at": true, "is": true, "are": true, "be": true,
		"this": true, "that": true, "it": true, "with": true, "as": true, "by": true,
		"from": true, "me": true, "my": true, "you": true, "your": true, "we": true,
		"our": true, "what": true, "why": true, "how": true,
	}
	// _kindRank puts rules first.
	_kindRank = map[string]int{
		KindConvention: 0, KindDecision: 1, KindLesson: 2, KindFinding: 3, KindFact: 4,
	}
)

// Recall returns the accepted items on the shelf that match any word of query, best match
// first. A limit of zero or less means 10.
func (s *Store) Recall(ctx context.Context, sh Shelf, query string, limit int) ([]Item, error) {
	words := terms(query)
	if len(words) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = _defaultLimit
	}
	where, args := shelfWhere(sh)
	args = append([]any{matchAny(words)}, args...)
	return queryItems(ctx, s.db,
		`JOIN memory_fts ON memory_fts.rowid = m.seq WHERE memory_fts MATCH ? AND `+where+
			` ORDER BY bm25(memory_fts), m.seq DESC LIMIT ?`, append(args, limit)...)
}

// Relevant returns up to limit accepted items on the shelf for the words of message, leaving
// out the ids in skip. An item scores one for each distinct word of the message found in its
// body or why. Best score first, then rules first, then newer; items that score zero are
// left out.
func (s *Store) Relevant(
	ctx context.Context, sh Shelf, message string, limit int, skip map[string]bool,
) ([]Item, error) {
	where, args := shelfWhere(sh)
	scores := map[string]int{}
	found := map[string]Item{}
	words := slices.Compact(slices.Sorted(slices.Values(terms(message))))
	words = slices.DeleteFunc(words, func(w string) bool { return _stopWords[w] })
	for _, w := range words[:min(len(words), _maxWords)] {
		items, err := queryItems(ctx, s.db,
			`WHERE m.seq IN (SELECT rowid FROM memory_fts WHERE memory_fts MATCH ?) AND `+where,
			append([]any{matchAny([]string{w})}, args...)...)
		if err != nil {
			return nil, err
		}
		for _, it := range items {
			if skip[it.ID] {
				continue
			}
			scores[it.ID]++
			found[it.ID] = it
		}
	}
	out := make([]Item, 0, len(found))
	for _, it := range found {
		out = append(out, it)
	}
	slices.SortFunc(out, func(a, b Item) int {
		switch {
		case scores[a.ID] != scores[b.ID]:
			return scores[b.ID] - scores[a.ID]
		case _kindRank[a.Kind] != _kindRank[b.Kind]:
			return _kindRank[a.Kind] - _kindRank[b.Kind]
		case !a.CreatedAt.Equal(b.CreatedAt):
			return b.CreatedAt.Compare(a.CreatedAt)
		}
		return strings.Compare(a.ID, b.ID)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// shelfWhere selects the accepted items of the memory table aliased m that the shelf may see.
func shelfWhere(sh Shelf) (string, []any) {
	where := `m.status = ? AND (m.scope = ?`
	args := []any{StatusAccepted, ScopeUser}
	if sh.RepoID != "" {
		where += ` OR (m.scope = ? AND m.scope_id = ?)`
		args = append(args, ScopeRepo, sh.RepoID)
	}
	if sh.BotID != "" {
		where += ` OR (m.scope = ? AND m.scope_id = ?)`
		args = append(args, ScopeBot, sh.BotID)
	}
	return where + `)`, args
}

// matchAny is an FTS5 query that matches any of words; words hold only letters and digits.
func matchAny(words []string) string {
	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = `"` + w + `"`
	}
	return strings.Join(quoted, " OR ")
}

func terms(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}
