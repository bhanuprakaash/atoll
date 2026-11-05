package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"atoll/provider"
)

type providerModel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type providerInfo struct {
	Name     string          `json:"name"`
	Label    string          `json:"label"`
	Found    bool            `json:"found"`
	Path     string          `json:"path"`
	Version  string          `json:"version"`
	SignedIn bool            `json:"signed_in"`
	Models   []providerModel `json:"models"`
}
type loginState struct {
	Status string `json:"status"`
	URL    string `json:"url"`
	Error  string `json:"error"`
}
type providers struct {
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	mu       sync.Mutex
	states   map[string]loginState
	signedIn map[string]bool
}

var _loginURL = regexp.MustCompile(`https://[^\s"'<>]+`)

func newProviders(parent context.Context) *providers {
	ctx, cancel := context.WithCancel(parent)
	return &providers{ctx: ctx, cancel: cancel, states: map[string]loginState{}, signedIn: map[string]bool{}}
}
func (p *providers) Close() { p.cancel(); p.wg.Wait() }
func (p *providers) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/providers")
	w.Header().Set("Content-Type", "application/json")
	if (path == "" && r.Method == http.MethodGet) || (path == "/refresh" && r.Method == http.MethodPost) {
		list := make([]providerInfo, 0, len(_adapters))
		for _, a := range _adapters {
			list = append(list, p.info(r.Context(), a))
		}
		_ = json.NewEncoder(w).Encode(list)
		return
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 2 || parts[1] != "sign-in" {
		http.NotFound(w, r)
		return
	}
	var a *adapter
	for i := range _adapters {
		if _adapters[i].name == parts[0] {
			a = &_adapters[i]
			break
		}
	}
	if a == nil {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	p.mu.Lock()
	state := p.states[a.name]
	if r.Method == http.MethodPost && state.Status != "starting" && state.Status != "waiting" {
		state = loginState{Status: "starting"}
		p.states[a.name] = state
		p.wg.Add(1)
		go p.login(*a)
	}
	p.mu.Unlock()
	if r.Method == http.MethodPost {
		w.WriteHeader(http.StatusAccepted)
	}
	_ = json.NewEncoder(w).Encode(state)
}
func (p *providers) info(ctx context.Context, a adapter) providerInfo {
	label := "Claude Code"
	if a.name == "codex" {
		label = "Codex"
	}
	info := providerInfo{Name: a.name, Label: label, Models: []providerModel{}}
	defer func() { p.setAvailable(a.name, info.SignedIn) }()
	bin := a.find(pathDirs(ctx))
	if bin == "" {
		return info
	}
	info.Found, info.Path = true, bin
	dir, err := a.dir()
	if err != nil {
		return info
	}
	info.Version, _ = provider.Version(ctx, bin)
	info.SignedIn, _ = a.signedIn(ctx, bin, dir)
	info.Models = probeModels(ctx, a.name, bin, dir)
	return info
}
func probeModels(parent context.Context, name, bin, dir string) []providerModel {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	args := []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose"}
	env := "CLAUDE_CONFIG_DIR=" + dir
	if name == "codex" {
		args = []string{"app-server"}
		env = "CODEX_HOME=" + dir
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = provider.Env(env)
	in, err := cmd.StdinPipe()
	if err != nil {
		return []providerModel{}
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return []providerModel{}
	}
	if err = cmd.Start(); err != nil {
		return []providerModel{}
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	if name == "claude" {
		_, _ = io.WriteString(in, `{"type":"control_request","request_id":"r1","request":{"subtype":"initialize"}}`+"\n")
	} else {
		_, _ = io.WriteString(in, `{"id":1,"method":"initialize","params":{"clientInfo":{"name":"atoll","version":"1"},"capabilities":{}}}`+"\n"+`{"id":2,"method":"model/list","params":{}}`+"\n")
	}
	scanner := bufio.NewScanner(out)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	for scanner.Scan() {
		var msg struct {
			Response struct {
				Response struct {
					Models []struct {
						Value string `json:"value"`
						Name  string `json:"displayName"`
					} `json:"models"`
				} `json:"response"`
			} `json:"response"`
			Result struct {
				Data []struct {
					ID   string `json:"id"`
					Name string `json:"displayName"`
				} `json:"data"`
			} `json:"result"`
		}
		if json.Unmarshal(scanner.Bytes(), &msg) != nil {
			continue
		}
		models := []providerModel{}
		for _, m := range msg.Response.Response.Models {
			models = append(models, providerModel{ID: m.Value, Name: m.Name})
		}
		for _, m := range msg.Result.Data {
			models = append(models, providerModel{ID: m.ID, Name: m.Name})
		}
		if len(models) > 0 {
			return models
		}
	}
	return []providerModel{}
}
func (p *providers) login(a adapter) {
	defer p.wg.Done()
	bin, dir, err := a.prepare(p.ctx)
	if err != nil {
		p.finish(a.name, err)
		return
	}
	cmd := a.login(p.ctx, bin, dir)
	read, write, err := os.Pipe()
	if err != nil {
		p.finish(a.name, err)
		return
	}
	defer func() { _ = read.Close(); _ = write.Close() }()
	cmd.Stdout, cmd.Stderr = write, write
	if err = cmd.Start(); err != nil {
		p.finish(a.name, err)
		return
	}
	_ = write.Close()
	scanner := bufio.NewScanner(read)
	for scanner.Scan() {
		if url := _loginURL.FindString(scanner.Text()); url != "" {
			p.mu.Lock()
			st := p.states[a.name]
			if st.URL == "" {
				st.URL, st.Status = url, "waiting"
				p.states[a.name] = st
			}
			p.mu.Unlock()
		}
	}
	err = cmd.Wait()
	if err == nil {
		signed, e := a.signedIn(p.ctx, bin, dir)
		if e != nil {
			err = e
		} else if !signed {
			err = fmt.Errorf("sign-in did not complete")
		}
	}
	p.finish(a.name, err)
}
func (p *providers) finish(name string, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	st := p.states[name]
	st.Status = "signed_in"
	if err != nil {
		st.Status, st.Error = "failed", err.Error()
	}
	p.states[name] = st
	if err == nil {
		p.signedIn[name] = true
	}
}

func (p *providers) setAvailable(name string, signed bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.signedIn[name] = signed
}

func (p *providers) available(ctx context.Context, name string) bool {
	p.mu.Lock()
	signed, ok := p.signedIn[name]
	p.mu.Unlock()
	if ok {
		return signed
	}
	return p.checkAvailable(ctx, name)
}

func (p *providers) checkAvailable(ctx context.Context, name string) bool {
	signed := available(ctx, name)
	p.setAvailable(name, signed)
	return signed
}
