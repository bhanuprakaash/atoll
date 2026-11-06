package fake

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// CodePrompt is what the real claude prints after its URL lines, with no newline.
	CodePrompt = "Paste code here if prompted > "
	// CodeOK is the one code LoginWithCode accepts.
	CodeOK = "reef-test-code"
	// CodeInvalid is what claude prints for any other code, before it exits 1.
	CodeInvalid = "Invalid code. Please make sure the full code was copied."
)

// Login plays a provider's own sign-in: it prints lines (one of them the sign-in URL), waits
// until <executable>.login.release exists (the user finishing in the browser), then writes file
// into dir as the login and exits 0.
func Login(dir, file string, lines ...string) error {
	return login(dir, file, false, lines)
}

// LoginWithCode is Login for a provider that also asks for the code its page shows: after lines
// it prints CodePrompt and finishes on the release file or on a stdin line. The line CodeOK signs
// in; any other prints CodeInvalid and exits 1. End of stdin does not end the login.
func LoginWithCode(dir, file string, lines ...string) error {
	return login(dir, file, true, lines)
}

func login(dir, file string, withCode bool, lines []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	for _, l := range lines {
		fmt.Println(l)
	}
	typed := make(chan string)
	if withCode {
		fmt.Print(CodePrompt)
		go func() {
			in := Lines()
			for in.Scan() {
				typed <- in.Text()
			}
		}()
	}
	for done := false; !done; {
		select {
		case l := <-typed:
			if strings.TrimSpace(l) != CodeOK {
				fmt.Println(CodeInvalid)
				os.Exit(1)
			}
			done = true
		case <-time.After(50 * time.Millisecond):
			_, err := os.Stat(exe + ".login.release")
			done = err == nil
		}
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

// Refused reports whether the login in dir/file is one the provider's server refuses: a file
// holding {"fake":"revoked"}. The fake has no server, so the file stands in for a token revoked
// there. Like a real CLI whose token its server revoked, the fake finds out only when a turn
// calls the model, and keeps the file: its status still reports signed in, and every later turn
// fails the same way.
func Refused(dir, file string) bool {
	b, err := os.ReadFile(filepath.Join(dir, file))
	return err == nil && Directive(b) == "revoked"
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
