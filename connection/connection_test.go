package connection

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPollToken(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   string
		err    string
	}{
		{name: "pending", status: 400, body: `{"error":"authorization_pending"}`, err: "pending"},
		{name: "slow down", status: 400, body: `{"error":"slow_down"}`, err: "pending"},
		{name: "success", status: 200, body: `{"access_token":"secret","refresh_token":"refresh","expires_in":3600}`, want: "secret"},
		{name: "access denied", status: 400, body: `{"error":"access_denied"}`, err: "access_denied"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/oauth/token" || r.ParseForm() != nil ||
					r.Form.Get("client_id") != ClientID || r.Form.Get("device_code") != "device" ||
					r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:device_code" {
					t.Error("incorrect token request")
				}
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer s.Close()
			got, err := PollToken(t.Context(), s.Client(), s.URL+"/", "device")
			if got.AccessToken != tt.want {
				t.Errorf("token = %q, want %q", got.AccessToken, tt.want)
			}
			if tt.err == "pending" && !errors.Is(err, ErrPending) ||
				tt.err == "access_denied" && (err == nil || !strings.Contains(err.Error(), tt.err)) ||
				tt.err == "" && err != nil {
				t.Errorf("error = %v, want %q", err, tt.err)
			}
		})
	}
}
