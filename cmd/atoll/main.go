// Command atoll is the Reef server.
package main

import (
	"fmt"
	"io"
	"os"
)

func main() {
	if err := run(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(w io.Writer) error {
	_, err := fmt.Fprintln(w, "atoll")
	return err
}
