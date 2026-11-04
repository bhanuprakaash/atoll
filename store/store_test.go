package store_test

import (
	"context"
	"slices"
	"testing"

	"atoll/store"
)

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
			th, err := st.CreateThread(ctx, bot.ID)
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
