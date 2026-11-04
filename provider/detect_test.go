package provider

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func writeBin(t *testing.T, path, script string, mode os.FileMode) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(script), mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFind(t *testing.T) {
	root := t.TempDir()
	first := writeBin(t, filepath.Join(root, "first", "tool"), "#!/bin/sh\n", 0o755)
	second := writeBin(t, filepath.Join(root, "second", "tool"), "#!/bin/sh\n", 0o755)
	plain := writeBin(t, filepath.Join(root, "plain", "tool"), "text", 0o644)
	if err := os.MkdirAll(filepath.Join(root, "asdir", "tool"), 0o755); err != nil {
		t.Fatal(err)
	}
	dir := func(name string) string { return filepath.Join(root, name) }

	tests := []struct {
		name     string
		override string
		dirs     []string
		want     string
	}{
		{"override wins", second, []string{dir("first")}, second},
		{"override not executable falls back", plain, []string{dir("first")}, first},
		{"override missing falls back", dir("none"), []string{dir("first")}, first},
		{"first dir wins", "", []string{dir("first"), dir("second")}, first},
		{"non-executable file skipped", "", []string{dir("plain"), dir("second")}, second},
		{"directory skipped", "", []string{dir("asdir"), dir("second")}, second},
		{"not found", "", []string{dir("plain"), dir("asdir"), dir("none")}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Find("tool", tt.override, tt.dirs); got != tt.want {
				t.Errorf("Find = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestVersion(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		want    string
		wantErr bool
	}{
		{"claude", "2.1.306 (Claude Code)", "2.1.306", false},
		{"codex", "codex-cli 0.166.0", "0.166.0", false},
		{"garbage", "usage: tool [options]", "", true},
		{"empty", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			script := "#!/bin/sh\necho '" + tt.output + "'\n"
			bin := writeBin(t, filepath.Join(t.TempDir(), "tool"), script, 0o755)
			got, err := Version(context.Background(), bin)
			if got != tt.want || (err != nil) != tt.wantErr {
				t.Errorf("Version = %q, %v; want %q, error %v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestDirs_ProviderPath(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	t.Setenv("ATOLL_PROVIDER_PATH", a+string(os.PathListSeparator)+b+string(os.PathListSeparator)+a)

	got := Dirs(context.Background())

	if want := []string{a, b}; !slices.Equal(got, want) {
		t.Errorf("Dirs = %q, want %q", got, want)
	}
}
