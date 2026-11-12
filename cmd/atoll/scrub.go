package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"atoll/provider"
)

const _tokenFD = "ATOLL_TOKEN_FD"

// scrubEnv keeps login tokens and the local API credential out of /proc/<pid>/environ, which a
// Bot's shell can read. os.Unsetenv does not change it, so a process that holds any of them
// re-executes itself with a clean environment (same pid and arguments). ATOLL_TOKEN crosses on an
// inherited pipe; the new process returns it, empty when there is none, and never puts it back in
// its environment, which every child process would inherit. It fails when the pipe cannot be read
// or is empty: serving without the credential would serve without auth.
func scrubEnv() (string, error) {
	if fd := os.Getenv(_tokenFD); fd != "" {
		_ = os.Unsetenv(_tokenFD)
		n, err := strconv.Atoi(fd)
		if err != nil {
			return "", fmt.Errorf("%s: %w", _tokenFD, err)
		}
		f := os.NewFile(uintptr(n), "token")
		b, err := io.ReadAll(f)
		_ = f.Close()
		if err != nil {
			return "", fmt.Errorf("read API token: %w", err)
		}
		if len(b) == 0 {
			return "", errors.New("read API token: empty")
		}
		return string(b), nil
	}
	token := os.Getenv("ATOLL_TOKEN")
	env := os.Environ()
	clean := slices.DeleteFunc(slices.Clone(env), func(e string) bool {
		return provider.Login(e) || strings.HasPrefix(e, "ATOLL_TOKEN")
	})
	if len(clean) == len(env) {
		return "", nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if token != "" {
		r, w, err := os.Pipe()
		if err != nil {
			return "", err
		}
		_, err = w.WriteString(token)
		_ = w.Close()
		if err != nil {
			return "", err
		}
		fd, err := syscall.Dup(int(r.Fd()))
		if err != nil {
			return "", err
		}
		clean = append(clean, _tokenFD+"="+strconv.Itoa(fd))
	}
	return "", syscall.Exec(exe, os.Args, clean)
}
