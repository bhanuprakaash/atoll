package main

import (
	"bytes"
	"testing"
)

func TestRun(t *testing.T) {
	var out bytes.Buffer
	if err := run(&out); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "atoll\n"; got != want {
		t.Errorf("run() wrote %q, want %q", got, want)
	}
}
