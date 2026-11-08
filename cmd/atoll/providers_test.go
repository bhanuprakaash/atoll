package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"atoll/provider"
	"atoll/store"
)

type fakeProvider struct {
	bin       string
	st        *store.Store
	probes    atomic.Int32
	launches  atomic.Int32
	providers *providers
}

// newFakeProvider returns providers over one adapter, "claude", whose executable reports version
// 1.2.3 and whose sign-in checks are counted.
func newFakeProvider(t *testing.T) *fakeProvider {
	t.Helper()
	bin := t.TempDir()
	script := "#!/bin/sh\n[ \"$1\" = --version ] && echo 1.2.3\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ATOLL_PROVIDER_PATH", bin)
	t.Setenv("ATOLL_DATA_DIR", t.TempDir())
	t.Setenv("ATOLL_CLAUDE_PATH", "")
	t.Setenv("PATH", os.Getenv("PATH")) // pathDirs rewrites PATH; restore it afterwards
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	f := &fakeProvider{bin: bin, st: st}
	adapters := []adapter{{
		name: "claude",
		new:  func() provider.Session { f.launches.Add(1); return nil },
		signedIn: func(context.Context, string, string) (bool, error) {
			f.probes.Add(1)
			return true, nil
		},
		own: func(string) provider.Own {
			return provider.Own{MCPServers: []string{"figma"}, Skills: []string{"pdf"}}
		},
	}}
	f.providers = newProviders(t.Context(), st, adapters)
	t.Cleanup(f.providers.Close)
	return f
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (f *fakeProvider) call(t *testing.T, method, path, body string) (int, []byte) {
	t.Helper()
	rec := httptest.NewRecorder()
	f.providers.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rec.Code, rec.Body.Bytes()
}

func (f *fakeProvider) get(t *testing.T) providerInfo {
	t.Helper()
	code, body := f.call(t, http.MethodGet, "/api/providers", "")
	var list []providerInfo
	if err := json.Unmarshal(body, &list); err != nil || code != http.StatusOK || len(list) != 1 {
		t.Fatalf("GET /api/providers = %d %s, %v; want one provider", code, body, err)
	}
	return list[0]
}

func TestProviders_GetServesCache(t *testing.T) {
	f := newFakeProvider(t)
	info := f.get(t)
	if !info.Found || info.Version != "1.2.3" || info.Path != filepath.Join(f.bin, "claude") || !info.SignedIn {
		t.Fatalf("GET after start-up = %+v; want found 1.2.3, signed in", info)
	}
	if got := strings.Join(info.OwnMCPServers, ",") + "|" + strings.Join(info.OwnSkills, ","); got != "figma|pdf" {
		t.Errorf("own lists = %q, want figma|pdf", got)
	}
	steps := []struct {
		name       string
		do         func()
		wantProbes int32
	}{
		{"two more GETs", func() { f.get(t); f.get(t) }, 1},
		{"Cached", func() { f.providers.Cached(t.Context()) }, 1},
		{"Refresh", func() { f.call(t, http.MethodPost, "/api/providers/refresh", "") }, 2},
		{"GET after Refresh", func() { f.get(t) }, 2},
	}
	for _, s := range steps {
		s.do()
		if got := f.probes.Load(); got != s.wantProbes {
			t.Errorf("after %s: %d sign-in probes, want %d", s.name, got, s.wantProbes)
		}
	}
}

func TestProviders_Update(t *testing.T) {
	f := newFakeProvider(t)
	other := filepath.Join(t.TempDir(), "claude")
	write(t, other, "#!/bin/sh\n[ \"$1\" = --version ] && echo 9.9.9\nexit 0\n")
	if err := os.Chmod(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := f.get(t); !got.Enabled || got.PathOverride != "" {
		t.Fatalf("default = enabled %v, path_override %q; want on and empty", got.Enabled, got.PathOverride)
	}
	tests := []struct {
		name        string
		give        string
		wantStatus  int
		wantEnabled bool
		wantPath    string
		wantVersion string
	}{
		{"switch off", `{"enabled":false}`, 200, false, "", "1.2.3"},
		{"path replaces the lookup", `{"path_override":"` + other + `"}`, 200, false, other, "9.9.9"},
		{"switch on keeps the path", `{"enabled":true}`, 200, true, other, "9.9.9"},
		{"relative path refused", `{"path_override":"claude"}`, 400, true, other, "9.9.9"},
		{"empty path clears it", `{"path_override":""}`, 200, true, "", "1.2.3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if code, body := f.call(t, http.MethodPut, "/api/providers/claude", tt.give); code != tt.wantStatus {
				t.Fatalf("PUT %s = %d %s, want %d", tt.give, code, body, tt.wantStatus)
			}
			got := f.get(t)
			if got.Enabled != tt.wantEnabled || got.PathOverride != tt.wantPath || got.Version != tt.wantVersion {
				t.Errorf("GET = enabled %v, path_override %q, version %q; want %v, %q, %q",
					got.Enabled, got.PathOverride, got.Version, tt.wantEnabled, tt.wantPath, tt.wantVersion)
			}
		})
	}
	if code, _ := f.call(t, http.MethodPut, "/api/providers/nope", `{}`); code != http.StatusNotFound {
		t.Errorf("PUT unknown provider = %d, want 404", code)
	}
}

func TestProviders_StoredSettingsSurviveRestart(t *testing.T) {
	f := newFakeProvider(t)
	if code, body := f.call(t, http.MethodPut, "/api/providers/claude", `{"enabled":false,"path_override":"/x/claude"}`); code != 200 {
		t.Fatalf("PUT = %d %s", code, body)
	}
	again := newProviders(t.Context(), f.st, f.providers.adapters)
	t.Cleanup(again.Close)
	got := again.Cached(t.Context())
	if len(got) != 1 || got[0].Enabled || again.available(t.Context(), "claude") ||
		again.pathOverride("claude") != "/x/claude" {
		t.Fatalf("after restart: %+v, path %q; want off with path /x/claude", got, again.pathOverride("claude"))
	}
}

func TestProviders_DisabledDoesNotLaunch(t *testing.T) {
	f := newFakeProvider(t)
	if code, body := f.call(t, http.MethodPut, "/api/providers/claude", `{"enabled":false}`); code != 200 {
		t.Fatalf("PUT = %d %s", code, body)
	}
	_, _, err := f.providers.launch(t.Context(), "claude")
	if err == nil || err.Error() != "claude is turned off in Settings > Providers" || f.launches.Load() != 0 {
		t.Fatalf("launch while off = %v with %d launches; want the turned-off error and none", err, f.launches.Load())
	}
	if f.providers.available(t.Context(), "claude") {
		t.Error("a provider that is off is available")
	}
	if code, body := f.call(t, http.MethodPut, "/api/providers/claude", `{"enabled":true}`); code != 200 {
		t.Fatalf("PUT = %d %s", code, body)
	}
	if _, cfg, err := f.providers.launch(t.Context(), "claude"); err != nil || f.launches.Load() != 1 ||
		cfg.Bin != filepath.Join(f.bin, "claude") {
		t.Fatalf("launch while on = %+v, %v with %d launches; want the binary and one launch", cfg, err, f.launches.Load())
	}
}
