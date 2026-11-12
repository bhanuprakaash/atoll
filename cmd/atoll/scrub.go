package main

import (
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
// inherited pipe; the new process reads it into its own memory.
func scrubEnv() error {
	if fd := os.Getenv(_tokenFD); fd != "" {
		_ = os.Unsetenv(_tokenFD)
		if n, err := strconv.Atoi(fd); err == nil {
			f := os.NewFile(uintptr(n), "token")
			if b, err := io.ReadAll(f); err == nil && len(b) > 0 {
				_ = os.Setenv("ATOLL_TOKEN", string(b))
			}
			_ = f.Close()
		}
		return nil
	}
	token := os.Getenv("ATOLL_TOKEN")
	env := os.Environ()
	clean := slices.DeleteFunc(slices.Clone(env), func(e string) bool {
		return provider.Login(e) || strings.HasPrefix(e, "ATOLL_TOKEN")
	})
	if len(clean) == len(env) {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if token != "" {
		r, w, err := os.Pipe()
		if err != nil {
			return err
		}
		_, err = w.WriteString(token)
		_ = w.Close()
		if err != nil {
			return err
		}
		fd, err := syscall.Dup(int(r.Fd()))
		if err != nil {
			return err
		}
		clean = append(clean, _tokenFD+"="+strconv.Itoa(fd))
	}
	return syscall.Exec(exe, os.Args, clean)
}
