// Command codex is a fake Codex for the e2e tests. It answers --version and `login status`,
// and as `codex app-server` speaks JSON-RPC on stdin and stdout, replaying the recordings in
// e2e/testdata/codex: a turn/start gets the recording whose prompt matches. Methods no recording
// has get an empty result. Signed in means CODEX_HOME (default ~/.codex) holds auth.json. Every
// call is logged to <executable>.log.
package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"atoll/e2e/fakes/fake"
)

const _version = "codex-cli 0.166.0"

type (
	entry struct {
		Dir string          `json:"dir"`
		Msg json.RawMessage `json:"msg"`
	}

	message struct {
		ID     json.RawMessage `json:"id,omitempty"`
		Method string          `json:"method,omitempty"`
		Params json.RawMessage `json:"params,omitempty"`
		Result json.RawMessage `json:"result,omitempty"`
		Error  json.RawMessage `json:"error,omitempty"`
	}

	// recording is one recorded app-server conversation.
	recording struct {
		thread  string
		prompt  string
		replies map[string]json.RawMessage // result of each request, by method
		turn    []json.RawMessage          // notifications after turn/start, through turn/completed
	}

	threadParams struct {
		ThreadID string `json:"threadId"`
		Input    []struct {
			Text string `json:"text"`
		} `json:"input"`
	}
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "fake codex:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	log, err := fake.Start()
	if err != nil {
		return err
	}
	switch {
	case slices.Contains(args, "--version"):
		fmt.Println(_version)
		return nil
	case slices.Contains(args, "login") && slices.Contains(args, "status"):
		return status()
	case slices.Contains(args, "app-server"):
		return serve(log)
	}
	return fmt.Errorf("unsupported arguments %q", args)
}

func status() error {
	dir := fake.ConfigDir("CODEX_HOME", ".codex")
	if _, err := os.Stat(filepath.Join(dir, "auth.json")); err != nil {
		fmt.Fprintln(os.Stderr, "Not logged in")
		os.Exit(1)
	}
	fmt.Println("Logged in using ChatGPT")
	return nil
}

func serve(log *fake.Log) error {
	recs, err := load()
	if err != nil {
		return err
	}
	var thread *recording
	in := fake.Lines()
	for in.Scan() {
		if err := log.Stdin(in.Text()); err != nil {
			return err
		}
		var m message
		if json.Unmarshal(in.Bytes(), &m) != nil || m.Method == "" || len(m.ID) == 0 {
			continue
		}
		var p threadParams
		_ = json.Unmarshal(m.Params, &p)
		switch m.Method {
		case "thread/start":
			thread = find(recs, func(r recording) bool { return r.replies[m.Method] != nil })
			reply(m.ID, thread.replies[m.Method], "")
		case "thread/resume":
			thread = find(recs, func(r recording) bool { return r.thread == p.ThreadID && r.replies[m.Method] != nil })
			if thread == nil {
				reply(m.ID, nil, fmt.Sprintf("no rollout found for thread id %s", p.ThreadID))
				continue
			}
			reply(m.ID, thread.replies[m.Method], "")
		case "turn/start":
			prompt := ""
			if len(p.Input) > 0 {
				prompt = p.Input[0].Text
			}
			rec := find(recs, func(r recording) bool {
				return r.prompt == prompt && thread != nil && r.thread == thread.thread
			})
			if rec == nil {
				reply(m.ID, nil, fmt.Sprintf("fake codex: no recording for prompt %q", prompt))
				continue
			}
			reply(m.ID, rec.replies[m.Method], "")
			for _, n := range rec.turn {
				fmt.Println(string(n))
			}
		default:
			result := json.RawMessage(`{}`)
			if r := find(recs, func(r recording) bool { return r.replies[m.Method] != nil }); r != nil {
				result = r.replies[m.Method]
			}
			reply(m.ID, result, "")
		}
	}
	return in.Err()
}

func reply(id, result json.RawMessage, errMsg string) {
	m := message{ID: id, Result: result}
	if errMsg != "" {
		m.Error, _ = json.Marshal(map[string]any{"code": -32600, "message": errMsg})
	}
	b, _ := json.Marshal(m)
	fmt.Println(string(b))
}

func find(recs []recording, ok func(recording) bool) *recording {
	for i := range recs {
		if ok(recs[i]) {
			return &recs[i]
		}
	}
	return nil
}

// load reads every recording, in file name order.
func load() ([]recording, error) {
	all, err := fake.Recordings("codex")
	if err != nil {
		return nil, err
	}
	recs := make([]recording, 0, len(all))
	for _, name := range slices.Sorted(maps.Keys(all)) {
		r, err := parse(all[name])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		recs = append(recs, r)
	}
	return recs, nil
}

func parse(lines []string) (recording, error) {
	r := recording{replies: map[string]json.RawMessage{}}
	methods := map[string]string{} // request id -> method
	inTurn := false
	for _, s := range lines {
		var e entry
		if err := json.Unmarshal([]byte(s), &e); err != nil {
			return r, err
		}
		var m message
		if err := json.Unmarshal(e.Msg, &m); err != nil {
			return r, err
		}
		switch {
		case e.Dir == "in" && m.Method != "" && len(m.ID) > 0:
			methods[string(m.ID)] = m.Method
			var p threadParams
			_ = json.Unmarshal(m.Params, &p)
			if m.Method == "turn/start" && len(p.Input) > 0 {
				r.prompt, r.thread = p.Input[0].Text, p.ThreadID
			}
		case e.Dir == "out" && m.Method == "" && len(m.ID) > 0:
			method := methods[string(m.ID)]
			r.replies[method] = m.Result
			inTurn = method == "turn/start"
		case e.Dir == "out" && inTurn:
			r.turn = append(r.turn, e.Msg)
		}
	}
	return r, nil
}
