package fake

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Login plays a provider's own sign-in: it prints lines (one of them the sign-in URL), waits
// until <executable>.login.release exists (the user finishing in the browser), then writes file
// into dir as the login and exits 0.
func Login(dir, file string, lines ...string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	for _, l := range lines {
		fmt.Println(l)
	}
	for {
		if _, err := os.Stat(exe + ".login.release"); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, file), []byte("{}"), 0o600); err != nil {
		return err
	}
	fmt.Println("Login successful.")
	return nil
}

// Pause is the "pause" directive: {"fake":"pause","ms":1500} holds a tool call that long, so a
// client can see each step of a run.
func Pause(ms int) {
	time.Sleep(time.Duration(ms) * time.Millisecond)
}

// PauseMS returns the ms of a pause directive line.
func PauseMS(line []byte) int {
	var d struct {
		MS json.Number `json:"ms"`
	}
	_ = json.Unmarshal(line, &d)
	n, _ := strconv.Atoi(d.MS.String())
	return n
}
