package memory

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	s, err := Open(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func save(t *testing.T, s *Store, n Note) Item {
	t.Helper()
	it, _, err := s.Remember(context.Background(), n)
	if err != nil {
		t.Fatal(err)
	}
	return it
}

func bodies(items []Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Body
	}
	return out
}

func TestRelevant(t *testing.T) {
	shelf := Shelf{RepoID: "r1", BotID: "b1"}
	notes := []Note{
		{Kind: KindLesson, Body: "Check SQLite journal locks before tuning.", Why: "Measure contention first.", Scope: ScopeBot, ScopeID: "b1"},
		{Kind: KindDecision, Body: "Use SQLite WAL for the journal.", Why: "Readers must not block journal writes.", Scope: ScopeRepo, ScopeID: "r1"},
		{Kind: KindConvention, Body: "SQLite journal changes require a rollback plan.", Why: "Keep recovery predictable.", Scope: ScopeUser},
		{Kind: KindFact, Body: "Durability needs fsync.", Scope: ScopeUser},
		{Kind: KindFact, Body: "Durability needs write barriers.", Scope: ScopeUser},
		{Kind: KindLesson, Body: "Other bot SQLite journal lesson.", Scope: ScopeBot, ScopeID: "b2"},
		{Kind: KindConvention, Body: "Other repo SQLite journal rule.", Scope: ScopeRepo, ScopeID: "r2"},
	}
	s := openTest(t)
	ids := map[string]string{}
	for _, n := range notes {
		ids[n.Body] = save(t, s, n).ID
	}
	retired := save(t, s, Note{Kind: KindConvention, Body: "Retired SQLite journal rule.", Scope: ScopeUser})
	if _, err := s.Retire(context.Background(), retired.ID); err != nil {
		t.Fatal(err)
	}
	convention := "SQLite journal changes require a rollback plan."
	decision := "Use SQLite WAL for the journal."
	lesson := "Check SQLite journal locks before tuning."

	tests := []struct {
		name    string
		message string
		limit   int
		skip    map[string]bool
		want    []string
	}{
		{
			name: "equal scores: rules first", limit: 10,
			message: "Recall the SQLite journal decision and explain why.",
			want:    []string{convention, decision, lesson},
		},
		{
			name: "equal scores and kinds: newer first", limit: 10, message: "durability",
			want: []string{"Durability needs write barriers.", "Durability needs fsync."},
		},
		{
			name: "more matching words first", limit: 10, message: "journal wal",
			want: []string{decision, convention, lesson},
		},
		{
			name: "a repeated word counts once", limit: 10, message: "wal wal wal journal",
			want: []string{decision, convention, lesson},
		},
		{name: "limit", limit: 2, message: "journal wal", want: []string{decision, convention}},
		{
			name: "skip applies before the limit", limit: 2, message: "journal wal",
			skip: map[string]bool{ids[decision]: true}, want: []string{convention, lesson},
		},
		{name: "stop words only", limit: 10, message: "Why the and how?", want: []string{}},
		{
			name: "the word cap keeps message order", limit: 10,
			message: "durability " + fillerWords(70),
			want:    []string{"Durability needs write barriers.", "Durability needs fsync."},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.Relevant(context.Background(), shelf, tt.message, tt.limit, tt.skip)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(bodies(got), tt.want) {
				t.Fatalf("got %q, want %q", bodies(got), tt.want)
			}
		})
	}
}

func TestRemember_Replaces(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	old := save(t, s, Note{Kind: KindDecision, Body: "use postgres", Scope: ScopeRepo, ScopeID: "r1"})
	other := save(t, s, Note{Kind: KindDecision, Body: "use mysql", Scope: ScopeRepo, ScopeID: "r2"})

	if _, _, err := s.Remember(ctx, Note{
		Kind: KindDecision, Body: "use sqlite", Scope: ScopeRepo, ScopeID: "r1", Replaces: []string{other.ID},
	}); err == nil {
		t.Fatal("replacing an item of another scope succeeded")
	}
	it := save(t, s, Note{
		Kind: KindDecision, Body: "use sqlite", Scope: ScopeRepo, ScopeID: "r1", Replaces: []string{old.ID},
	})
	if !slices.Equal(it.Replaces, []string{old.ID}) || it.Status != StatusAccepted {
		t.Fatalf("new item = %+v", it)
	}
	got, err := s.Read(ctx, old.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusRetired || got.ReplacedBy != it.ID || got.RetiredAt == nil {
		t.Fatalf("old item = %+v", got)
	}
	if _, _, err := s.Remember(ctx, Note{
		Kind: KindDecision, Body: "use duckdb", Scope: ScopeRepo, ScopeID: "r1", Replaces: []string{old.ID},
	}); err == nil {
		t.Fatal("replacing a retired item succeeded")
	}
	all, err := s.List(ctx)
	if err != nil || len(all) != 3 {
		t.Fatalf("list = %d items, err %v; the failed saves must write nothing", len(all), err)
	}
}

// fillerWords returns n distinct words that sort before every word in the notes.
func fillerWords(n int) string {
	words := make([]string, n)
	for i := range words {
		words[i] = fmt.Sprintf("aa%03d", i)
	}
	return strings.Join(words, " ")
}
