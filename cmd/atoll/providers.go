package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"atoll/provider"
	"atoll/provider/claude"
	"atoll/server"
	"atoll/store"
)

type providerInfo struct {
	server.Provider
	Found         bool     `json:"found"`
	Path          string   `json:"path"`
	PathOverride  string   `json:"path_override"`
	Version       string   `json:"version"`
	OwnMCPServers []string `json:"own_mcp_servers"`
	OwnSkills     []string `json:"own_skills"`
}
type loginState struct {
	Status    string `json:"status"`
	URL       string `json:"url"`
	NeedsCode bool   `json:"needs_code"`
	Error     string `json:"error"`
}
type providers struct {
	st       *store.Store
	adapters []adapter
	ready    chan struct{} // closed once the start-up check has run
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	mu       sync.Mutex
	states   map[string]loginState
	codes    map[string]io.Writer
	signedIn map[string]bool
	infos    map[string]providerInfo // what the last check found
	expired  map[string]bool         // refused a run: signed out until a sign-in or Refresh
	off      map[string]bool         // switched off in Settings, as stored
	paths    map[string]string       // path overrides, as stored
}

var _loginURL = regexp.MustCompile(`https://[^\s"'<>]+`)

// newProviders returns the providers and checks each of them in the background.
func newProviders(parent context.Context, st *store.Store, adapters []adapter) *providers {
	ctx, cancel := context.WithCancel(parent)
	p := &providers{st: st, adapters: slices.Clone(adapters), ready: make(chan struct{}), ctx: ctx,
		cancel: cancel, states: map[string]loginState{}, codes: map[string]io.Writer{},
		signedIn: map[string]bool{}, infos: map[string]providerInfo{}, expired: map[string]bool{},
		off: map[string]bool{}, paths: map[string]string{}}
	for _, a := range adapters {
		p.infos[a.name] = blank(a)
		on, _ := st.Setting(parent, settingKey(a.name, "enabled"))
		p.off[a.name] = on == "false"
		p.paths[a.name], _ = st.Setting(parent, settingKey(a.name, "path"))
	}
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		defer close(p.ready)
		for _, a := range adapters {
			p.info(ctx, a)
		}
	}()
	return p
}
func (p *providers) Close() { p.cancel(); p.wg.Wait() }
func (p *providers) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/providers")
	w.Header().Set("Content-Type", "application/json")
	if (path == "" && r.Method == http.MethodGet) || (path == "/refresh" && r.Method == http.MethodPost) {
		refresh := path == "/refresh"
		if refresh {
			p.mu.Lock()
			clear(p.expired)
			p.mu.Unlock()
		}
		list := make([]providerInfo, 0, len(p.adapters))
		for _, a := range p.adapters {
			if refresh {
				p.info(r.Context(), a)
			}
			list = append(list, p.view(r.Context(), a))
		}
		_ = json.NewEncoder(w).Encode(list)
		return
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 1 && r.Method == http.MethodPut {
		p.update(w, r, parts[0])
		return
	}
	if len(parts) == 3 && parts[1] == "sign-in" && parts[2] == "code" && r.Method == http.MethodPost {
		p.submitCode(w, r, parts[0])
		return
	}
	if len(parts) != 2 || parts[1] != "sign-in" {
		http.NotFound(w, r)
		return
	}
	a := p.adapter(parts[0])
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

func (p *providers) adapter(name string) *adapter {
	for i := range p.adapters {
		if p.adapters[i].name == name {
			return &p.adapters[i]
		}
	}
	return nil
}

// update stores the switch and the path override a PUT body names, and checks the provider again
// when its path changed.
func (p *providers) update(w http.ResponseWriter, r *http.Request, name string) {
	a := p.adapter(name)
	if a == nil {
		http.NotFound(w, r)
		return
	}
	var body struct {
		Enabled      *bool   `json:"enabled"`
		PathOverride *string `json:"path_override"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON body"})
		return
	}
	if path := body.PathOverride; path != nil && *path != "" && !filepath.IsAbs(*path) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "path_override must be an absolute path"})
		return
	}
	var err error
	if body.Enabled != nil {
		err = p.st.SetSetting(r.Context(), settingKey(name, "enabled"), strconv.FormatBool(*body.Enabled))
		if err == nil {
			p.mu.Lock()
			p.off[name] = !*body.Enabled
			p.mu.Unlock()
		}
	}
	if err == nil && body.PathOverride != nil {
		err = p.st.SetSetting(r.Context(), settingKey(name, "path"), *body.PathOverride)
		if err == nil {
			p.mu.Lock()
			p.paths[name] = *body.PathOverride
			p.mu.Unlock()
		}
	}
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	if body.PathOverride != nil {
		p.info(r.Context(), *a)
	}
	_ = json.NewEncoder(w).Encode(p.view(r.Context(), *a))
}

func settingKey(name, field string) string { return "provider_" + name + "_" + field }

func (p *providers) enabled(name string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return !p.off[name]
}

func (p *providers) pathOverride(name string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.paths[name]
}

// Cached lists every provider as the last check found it, without a new one.
func (p *providers) Cached(ctx context.Context) []server.Provider {
	list := make([]server.Provider, 0, len(p.adapters))
	for _, a := range p.adapters {
		list = append(list, p.view(ctx, a).Provider)
	}
	return list
}

// view is what the last check found for a, with the stored switch and path override. It waits for
// the start-up check.
func (p *providers) view(ctx context.Context, a adapter) providerInfo {
	select {
	case <-p.ready:
	case <-ctx.Done():
	}
	p.mu.Lock()
	info := p.infos[a.name]
	info.SignedIn = p.signedIn[a.name]
	info.Enabled = !p.off[a.name]
	info.PathOverride = p.paths[a.name]
	p.mu.Unlock()
	info.Models = slices.Clone(info.Models)
	info.OwnMCPServers = slices.Clone(info.OwnMCPServers)
	info.OwnSkills = slices.Clone(info.OwnSkills)
	return info
}

func blank(a adapter) providerInfo {
	label := "Claude Code"
	if a.name == "codex" {
		label = "Codex"
	}
	return providerInfo{
		Provider:      server.Provider{Name: a.name, Label: label, Models: []server.Model{}},
		OwnMCPServers: []string{}, OwnSkills: []string{},
	}
}

func (p *providers) info(ctx context.Context, a adapter) (info providerInfo) {
	info = blank(a)
	if home, err := os.UserHomeDir(); err == nil {
		own := a.own(home)
		info.OwnMCPServers, info.OwnSkills = own.MCPServers, own.Skills
	}
	defer func() {
		info.SignedIn = p.setAvailable(a.name, info.SignedIn)
		p.mu.Lock()
		p.infos[a.name] = info
		p.mu.Unlock()
	}()
	bin := a.find(pathDirs(ctx), p.pathOverride(a.name))
	if bin == "" {
		return info
	}
	info.Found, info.Path = true, bin
	dir, err := a.dir()
	if err != nil {
		return info
	}
	// Codex refuses a CODEX_HOME that does not exist, and a provider not yet signed in has none.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return info
	}
	info.Version, _ = provider.Version(ctx, bin)
	info.SignedIn, _ = a.signedIn(ctx, bin, dir)
	info.Models = probeModels(ctx, a.name, bin, dir)
	return info
}
func probeModels(parent context.Context, name, bin, dir string) []server.Model {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	args := []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose"}
	env := "CLAUDE_CONFIG_DIR=" + dir
	if name == "claude" {
		cwd, err := os.Getwd()
		if err != nil {
			return []server.Model{}
		}
		isolation, err := claude.IsolationArgs(cwd)
		if err != nil {
			return []server.Model{}
		}
		args = append(args, isolation...)
	}
	if name == "codex" {
		args = []string{"app-server"}
		env = "CODEX_HOME=" + dir
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = provider.Env(env)
	if name == "claude" {
		cmd.Env = append(cmd.Env, "ENABLE_CLAUDEAI_MCP_SERVERS=false", "CLAUDE_CODE_SHELL_PREFIX="+filepath.Join(dir, "..", "..", "bin", "claude-shell-prefix"))
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		return []server.Model{}
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return []server.Model{}
	}
	if err = cmd.Start(); err != nil {
		return []server.Model{}
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
		models := []server.Model{}
		for _, m := range msg.Response.Response.Models {
			models = append(models, server.Model{ID: m.Value, Name: m.Name})
		}
		for _, m := range msg.Result.Data {
			models = append(models, server.Model{ID: m.ID, Name: m.Name})
		}
		if len(models) > 0 {
			return models
		}
	}
	return []server.Model{}
}
func (p *providers) login(a adapter) {
	defer p.wg.Done()
	bin, dir, err := a.prepare(p.ctx, p.pathOverride(a.name))
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
	stdin, err := cmd.StdinPipe()
	if err != nil {
		p.finish(a.name, err)
		return
	}
	if err = cmd.Start(); err != nil {
		p.finish(a.name, err)
		return
	}
	_ = write.Close()
	p.mu.Lock()
	p.codes[a.name] = stdin
	p.mu.Unlock()
	scanner := bufio.NewScanner(read)
	// claude's "Paste code here if prompted > " has no newline, so a line also ends at ">".
	scanner.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		if i := bytes.IndexAny(data, "\n>"); i >= 0 {
			return i + 1, data[:i], nil
		}
		if atEOF && len(data) > 0 {
			return len(data), data, nil
		}
		return 0, nil, nil
	})
	var last string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			last = line
		}
		p.mu.Lock()
		st := p.states[a.name]
		if url := _loginURL.FindString(line); url != "" && st.URL == "" {
			st.URL, st.Status = url, "waiting"
		}
		if strings.Contains(line, "Paste code") {
			st.NeedsCode = true
		}
		p.states[a.name] = st
		p.mu.Unlock()
	}
	err = cmd.Wait()
	if err != nil && last != "" && p.ctx.Err() == nil {
		err = errors.New(last)
	}
	if err == nil {
		signed, e := a.signedIn(p.ctx, bin, dir)
		if e != nil {
			err = e
		} else if !signed {
			err = fmt.Errorf("sign-in did not complete")
		}
	}
	if err == nil {
		p.info(p.ctx, a)
	}
	p.finish(a.name, err)
}
func (p *providers) finish(name string, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.codes, name)
	st := p.states[name]
	st.Status, st.NeedsCode = "signed_in", false
	if err != nil {
		st.Status, st.Error = "failed", err.Error()
	}
	p.states[name] = st
	if err == nil {
		p.signedIn[name] = true
		delete(p.expired, name)
	}
}

func (p *providers) submitCode(w http.ResponseWriter, r *http.Request, name string) {
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Code) == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "code is required"})
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	stdin, ok := p.codes[name]
	if !ok || !p.states[name].NeedsCode {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "no sign-in is waiting for a code"})
		return
	}
	if _, err := io.WriteString(stdin, strings.TrimSpace(body.Code)+"\n"); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// setAvailable caches the result of a status check; a provider that refused a run stays signed out.
func (p *providers) setAvailable(name string, signed bool) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	signed = signed && !p.expired[name]
	p.signedIn[name] = signed
	return signed
}

// signOut marks a provider that refused a run signed out until a sign-in finishes or Refresh checks it.
func (p *providers) signOut(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.expired[name] = true
	p.signedIn[name] = false
}

func (p *providers) available(ctx context.Context, name string) bool {
	if !p.enabled(name) {
		return false
	}
	p.mu.Lock()
	signed, ok := p.signedIn[name]
	p.mu.Unlock()
	if ok {
		return signed
	}
	return p.checkAvailable(ctx, name)
}

func (p *providers) checkAvailable(ctx context.Context, name string) bool {
	return p.setAvailable(name, p.signedInNow(ctx, name))
}

// signedInNow asks the provider whether it is installed and signed in.
func (p *providers) signedInNow(ctx context.Context, name string) bool {
	a := p.adapter(name)
	if a == nil {
		return false
	}
	bin, dir, err := a.prepare(ctx, p.pathOverride(name))
	if err != nil {
		return false
	}
	signed, err := a.signedIn(ctx, bin, dir)
	return err == nil && signed
}

// launch returns a session for the provider name with its binary and settings folder. A provider
// that is turned off does not launch.
func (p *providers) launch(ctx context.Context, name string) (provider.Session, provider.Config, error) {
	a := p.adapter(name)
	if a == nil {
		return nil, provider.Config{}, fmt.Errorf("unknown provider %q", name)
	}
	if !p.enabled(name) {
		return nil, provider.Config{}, fmt.Errorf("%s is turned off in Settings > Providers", name)
	}
	p.checkAvailable(ctx, name)
	bin, dir, err := a.prepare(ctx, p.pathOverride(name))
	if err != nil {
		return nil, provider.Config{}, err
	}
	return a.new(), provider.Config{Bin: bin, Dir: dir}, nil
}
