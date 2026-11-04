package store_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"atoll/store"
)

func TestStore_Open_Private(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.CreateBot(t.Context(), store.Bot{Name: "a", Persona: "p", Provider: "claude"}); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]os.FileMode{
		dir:                                0o700,
		filepath.Join(dir, "atoll.db"):     0o600,
		filepath.Join(dir, "atoll.db-wal"): 0o600,
	} {
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := fi.Mode().Perm(); got != want {
			t.Errorf("%s mode %v, want %v", path, got, want)
		}
	}
}

func TestRepoAccess_JSON(t *testing.T) {
	tests := []struct {
		name    string
		give    string
		wantAll bool
		want    string
	}{
		{"all", `"all"`, true, `"all"`},
		{"null is all", `null`, true, `"all"`},
		{"list", `["a","b"]`, false, `["a","b"]`},
		{"empty list allows none", `[]`, false, `[]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got store.RepoAccess
			if err := json.Unmarshal([]byte(tt.give), &got); err != nil {
				t.Fatal(err)
			}
			if (got == nil) != tt.wantAll {
				t.Fatalf("nil = %v, want %v", got == nil, tt.wantAll)
			}
			out, err := json.Marshal(got)
			if err != nil || string(out) != tt.want {
				t.Fatalf("marshal = %s, %v; want %s", out, err, tt.want)
			}
		})
	}
}

func TestStore_FailUnfinished(t *testing.T) {
	tests := []struct {
		name string
		give []string
		want []string
	}{
		{"running and waiting fail", []string{"running", "waiting"}, []string{"running", "waiting"}},
		{"finished stay", []string{"done", "failed", "running"}, []string{"running"}},
		{"none", []string{"done"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			dir := t.TempDir()
			st, err := store.Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			bot, _ := st.CreateBot(ctx, store.Bot{Name: "a", Persona: "p", Provider: "claude"})
			th, err := st.CreateThread(ctx, bot.ID, "")
			if err != nil {
				t.Fatal(err)
			}
			byStatus := map[string]string{}
			for _, status := range tt.give {
				_, r, err := st.StartRun(ctx, th.ID, "hi")
				if err != nil {
					t.Fatal(err)
				}
				switch status {
				case "waiting":
					err = st.SetStatus(ctx, r.ID, store.StatusWaiting)
				case "done", "failed":
					err = st.FinishRun(ctx, r.ID, status, "", "")
				}
				if err != nil {
					t.Fatal(err)
				}
				byStatus[status] = r.ID
			}

			got, err := st.FailUnfinished(ctx, "restart")
			if err != nil {
				t.Fatal(err)
			}
			var want []string
			for _, status := range tt.want {
				want = append(want, byStatus[status])
			}
			slices.Sort(got)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Fatalf("ids = %v, want %v", got, want)
			}
			for _, id := range want {
				r, err := st.Run(ctx, id)
				if err != nil || r.Status != store.StatusFailed || r.Error != "restart" {
					t.Fatalf("run %s = %+v, %v", id, r, err)
				}
			}
		})
	}
}
