package connection

import "testing"

func TestOnGitHub(t *testing.T) {
	tests := []struct {
		name   string
		remote string
		want   bool
	}{
		{"https", "https://github.com/octo/hello.git", true},
		{"ssh url", "ssh://git@github.com/octo/hello.git", true},
		{"scp form", "git@github.com:octo/hello.git", true},
		{"other host", "https://gitlab.example/octo/hello.git", false},
		{"local path", "/srv/git/hello.git", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ATOLL_GITHUB_URL", "")
			if got := OnGitHub(tt.remote); got != tt.want {
				t.Errorf("OnGitHub(%q) = %v, want %v", tt.remote, got, tt.want)
			}
		})
	}
}

func TestOnGitHub_TestSeamPort(t *testing.T) {
	t.Setenv("ATOLL_GITHUB_URL", "http://127.0.0.1:41234")
	if !OnGitHub("http://127.0.0.1:41234/octo/hello.git") || OnGitHub("http://127.0.0.1:9/octo/hello.git") {
		t.Error("host and port must both match ATOLL_GITHUB_URL")
	}
}

func TestGitHubRepo(t *testing.T) {
	for _, remote := range []string{"https://github.com/octo/hello.git", "ssh://git@github.com/octo/hello", "git@github.com:octo/hello.git", "http://github.com/octo/hello/"} {
		if o, n, ok := GitHubRepo(remote); !ok || o != "octo" || n != "hello" {
			t.Errorf("GitHubRepo(%q) = %q, %q, %v", remote, o, n, ok)
		}
	}
	for _, remote := range []string{"https://gitlab.example/octo/hello.git", "https://github.com/octo", "https://github.com/a/b/c", ""} {
		if _, _, ok := GitHubRepo(remote); ok {
			t.Errorf("GitHubRepo(%q) ok", remote)
		}
	}
}
