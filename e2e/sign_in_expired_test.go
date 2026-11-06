package e2e

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// Contract: sign-in-expired-contract.md.

var _credentials = map[string]string{"claude": ".credentials.json", "codex": "auth.json"}

func TestSignIn_RefusedStaysSignedOutUntilRefresh(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m, s := start(t, p)
			if !s.providers(t)[p].SignedIn {
				t.Fatalf("%s not signed in before the refused turn", p)
			}
			creds := filepath.Join(m.providerDir(p), _credentials[p])
			writeFile(t, creds, `{"fake":"revoked"}`)

			th := s.newThread(t, s.newBot(t, "Kelp", p))
			ch := s.events(t, th)
			r := s.send(t, th, _hey)
			until(t, ch, r.ID, "run_finished")
			var run struct {
				Status    string `json:"status"`
				ErrorKind string `json:"error_kind"`
			}
			s.do(t, "GET", "/api/runs/"+r.ID, nil, &run, http.StatusOK)
			if run.Status != "failed" || run.ErrorKind != "signed_out" {
				t.Fatalf("run on a refused login: %+v, want failed with error_kind signed_out", run)
			}
			// The fake keeps its login and its status check still says signed in, as a real CLI
			// with a revoked token does.
			if b, err := os.ReadFile(creds); err != nil || string(b) != `{"fake":"revoked"}` {
				t.Fatalf("fake dropped its login: %q, %v", b, err)
			}

			for range 2 {
				if s.providers(t)[p].SignedIn {
					t.Fatalf("GET /api/providers shows %s signed in after the refused turn", p)
				}
			}

			var ps []providerInfo
			s.do(t, "POST", "/api/providers/refresh", nil, &ps, http.StatusOK)
			for _, info := range ps {
				if info.Name == p && !info.SignedIn {
					t.Errorf("POST /api/providers/refresh after a successful status check: %s still signed out", p)
				}
			}
			if !s.providers(t)[p].SignedIn {
				t.Errorf("GET /api/providers after Refresh: %s still signed out", p)
			}
		})
	}
}
