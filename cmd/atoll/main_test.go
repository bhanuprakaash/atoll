package main

import (
	"bytes"
	"context"
	"testing"
)

func TestRun(t *testing.T) {
	var out bytes.Buffer
	if err := run(context.Background(), nil, &out); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "atoll\n"; got != want {
		t.Errorf("run() wrote %q, want %q", got, want)
	}
}
