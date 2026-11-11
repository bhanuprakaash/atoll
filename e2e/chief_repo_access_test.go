package e2e

import (
	"database/sql"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// Contract: prompt-contract.md → Chief and API.

// dbExec runs stmts on the data dir's database while no server runs, as an older Reef would
// have left it, and returns the rows the last one affected.
func dbExec(t *testing.T, m *machine, stmts ...string) int64 {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(m.data, "atoll.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var n int64
	for _, stmt := range stmts {
		res, err := db.ExecContext(t.Context(), stmt)
		if err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
		n, _ = res.RowsAffected()
	}
	return n
}

// dbCount returns the single integer query returns.
func dbCount(t *testing.T, m *machine, query string) int {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(m.data, "atoll.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var n int
	if err := db.QueryRowContext(t.Context(), query).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func TestChief_RepoAccessRefused(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			path := t.TempDir()
			gitIn(t, path, "init", "-b", "main")
			var repo registeredRepo
			s.do(t, "POST", "/api/repos", map[string]string{"name": "seeded", "path": path}, &repo, http.StatusCreated)

			for _, access := range []any{"all", []string{repo.ID}} {
				var refused struct {
					Error string `json:"error"`
				}
				s.do(t, "PUT", "/api/bots/chief", map[string]any{"repo_access": access}, &refused, http.StatusBadRequest)
				if !strings.Contains(strings.ToLower(refused.Error), "repo") {
					t.Errorf("PUT /api/bots/chief repo_access %v: error %q, want a plain message about repo access", access, refused.Error)
				}
				if got := string(s.bot(t, "chief").RepoAccess); got != "[]" {
					t.Fatalf("Chief repo_access after PUT %v: %s, want []", access, got)
				}

				// edit_bot's repo_access is an array or null ("all").
				args := access
				if access == "all" {
					args = nil
				}
				c := chiefCall(t, s, "edit_bot", map[string]any{"id": "chief", "repo_access": args})
				failsListing(t, c, []string{refused.Error})
				var changes []any
				s.do(t, "GET", "/api/threads/"+c.thread+"/changes", nil, &changes, http.StatusOK)
				if len(changes) != 0 {
					t.Errorf("refused edit_bot recorded changes: %v", changes)
				}
				if got := string(s.bot(t, "chief").RepoAccess); got != "[]" {
					t.Fatalf("Chief repo_access after edit_bot %v: %s, want []", args, got)
				}
			}
		})
	}
}

func TestChief_LaunchWritesNothingWhenNoRepos(t *testing.T) {
	t.Parallel()
	m, s := start(t, "claude")
	s.kill()
	dbExec(t, m,
		`CREATE TABLE e2e_chief_writes (n INTEGER)`,
		`CREATE TRIGGER e2e_chief_written AFTER UPDATE ON bots WHEN OLD.id = 'chief'
			BEGIN INSERT INTO e2e_chief_writes VALUES (1); END`)

	s = m.serve(t)
	if got := string(s.bot(t, "chief").RepoAccess); got != "[]" {
		t.Fatalf("Chief repo_access %s, want []", got)
	}
	s.kill()
	if n := dbCount(t, m, `SELECT count(*) FROM e2e_chief_writes`); n != 0 {
		t.Errorf("a launch with Chief already on [] wrote Chief's row %d times, want none", n)
	}

	// The probe sees a write: a launch that has to move Chief to [] trips it.
	dbExec(t, m, `UPDATE bots SET repo_access = NULL WHERE id = 'chief'`, `DELETE FROM e2e_chief_writes`)
	s = m.serve(t)
	if got := string(s.bot(t, "chief").RepoAccess); got != "[]" {
		t.Fatalf("Chief repo_access after an old install's launch %s, want []", got)
	}
	s.kill()
	if n := dbCount(t, m, `SELECT count(*) FROM e2e_chief_writes`); n == 0 {
		t.Fatal("moving Chief to [] left no write: the probe does not work")
	}
}
