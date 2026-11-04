package memory

import (
	"cmp"
	"context"
	"fmt"
	"slices"
)

const (
	_similarCosine    = 0.92
	_similarTermShare = 0.7
)

// lookalikes reports the accepted items of the scope of n that look like it: by cosine when
// vec is set, else when the best word match shares most of its words.
func (s *Store) lookalikes(ctx context.Context, n Note, vec []float32) ([]Similar, error) {
	if vec != nil {
		return s.nearVectors(ctx, n, vec)
	}
	words := terms(n.Body)
	if len(words) == 0 {
		return nil, nil
	}
	hits, err := queryItems(ctx, s.db,
		`JOIN memory_fts ON memory_fts.rowid = m.seq
		 WHERE memory_fts MATCH ? AND m.status = ? AND m.scope = ? AND m.scope_id = ?
		 ORDER BY bm25(memory_fts), m.seq DESC`,
		matchAny(words), StatusAccepted, n.Scope, n.ScopeID)
	if err != nil {
		return nil, fmt.Errorf("search lookalikes: %w", err)
	}
	want := slices.Compact(slices.Sorted(slices.Values(words)))
	for _, h := range hits {
		if slices.Contains(n.Replaces, h.ID) {
			continue
		}
		have := terms(h.Body)
		found := 0
		for _, w := range want {
			if slices.Contains(have, w) {
				found++
			}
		}
		share := float64(found) / float64(len(want))
		if share >= _similarTermShare {
			return []Similar{{ID: h.ID, Body: h.Body, Score: share}}, nil
		}
		break
	}
	return nil, nil
}

func (s *Store) nearVectors(ctx context.Context, n Note, vec []float32) ([]Similar, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, body, vector FROM memory
		 WHERE vector IS NOT NULL AND status = ? AND scope = ? AND scope_id = ?`,
		StatusAccepted, n.Scope, n.ScopeID)
	if err != nil {
		return nil, fmt.Errorf("query vectors: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Similar
	for rows.Next() {
		var (
			sim  Similar
			blob []byte
		)
		if err := rows.Scan(&sim.ID, &sim.Body, &blob); err != nil {
			return nil, fmt.Errorf("scan vector: %w", err)
		}
		v := decodeVector(blob)
		if len(v) != len(vec) || slices.Contains(n.Replaces, sim.ID) {
			continue
		}
		if sim.Score = cosine(vec, v); sim.Score >= _similarCosine {
			out = append(out, sim)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read vectors: %w", err)
	}
	slices.SortFunc(out, func(a, b Similar) int { return cmp.Compare(b.Score, a.Score) })
	return out, nil
}
