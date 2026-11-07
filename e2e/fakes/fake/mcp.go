package fake

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Pace is how long every tool call takes, so a client can see the run stream.
const Pace = 300 * time.Millisecond

// Hang blocks until the process is killed, as a stuck provider does.
func Hang() {
	select {}
}

// MCP is a client of one MCP server, as the real CLIs speak it: streamable HTTP, or a local
// process on stdio.
type MCP struct {
	url     string
	headers map[string]string
	session string
	Tools   map[string]bool
	Params  map[string][]string // each tool's inputSchema property names
	next    int
	stdin   io.Writer // a stdio server: its process and the lines it prints
	stdout  *bufio.Scanner
}

// Dial does the MCP handshake (initialize, notifications/initialized, tools/list).
func Dial(url string, headers map[string]string) (*MCP, error) {
	return (&MCP{url: url, headers: headers}).handshake()
}

// DialStdio starts a local MCP server as the real CLIs do (its env is ours plus env) and does the
// handshake. The server ends when this process does: its stdin closes.
func DialStdio(command string, args []string, env map[string]string) (*MCP, error) {
	cmd := exec.Command(command, args...)
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcp start %s: %w", command, err)
	}
	c := &MCP{url: command, stdin: stdin, stdout: bufio.NewScanner(stdout)}
	c.stdout.Buffer(nil, 64*1024*1024)
	return c.handshake()
}

func (c *MCP) handshake() (*MCP, error) {
	url := c.url
	if _, err := c.request("initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "fake", "version": "0"},
	}); err != nil {
		return nil, fmt.Errorf("mcp initialize %s: %w", url, err)
	}
	if err := c.notify("notifications/initialized"); err != nil {
		return nil, err
	}
	listed, err := c.request("tools/list", map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("mcp tools/list %s: %w", url, err)
	}
	var listing struct {
		Tools []struct {
			Name        string `json:"name"`
			InputSchema struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(listed, &listing); err != nil {
		return nil, err
	}
	c.Tools, c.Params = map[string]bool{}, map[string][]string{}
	for _, tool := range listing.Tools {
		c.Tools[tool.Name] = true
		for name := range tool.InputSchema.Properties {
			c.Params[tool.Name] = append(c.Params[tool.Name], name)
		}
	}
	return c, nil
}

// Call runs one tool and returns the text of its result. It waits as long as the server takes:
// a permission prompt tool answers only when the user does.
func (c *MCP) Call(tool string, args json.RawMessage) (text string, isError bool, err error) {
	res, err := c.request("tools/call", map[string]any{"name": tool, "arguments": args})
	if err != nil {
		return "", false, fmt.Errorf("mcp tools/call %s: %w", tool, err)
	}
	var r struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return "", false, fmt.Errorf("mcp tools/call %s result: %w", tool, err)
	}
	var parts []string
	for _, p := range r.Content {
		if p.Type == "text" {
			parts = append(parts, p.Text)
		}
	}
	return strings.Join(parts, "\n"), r.IsError, nil
}

func (c *MCP) notify(method string) error {
	if c.stdin != nil {
		return c.send(map[string]any{"jsonrpc": "2.0", "method": method})
	}
	resp, err := c.post(map[string]any{"jsonrpc": "2.0", "method": method})
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

func (c *MCP) send(msg any) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = c.stdin.Write(append(b, '\n'))
	return err
}

func (c *MCP) request(method string, params any) (json.RawMessage, error) {
	c.next++
	id := c.next
	if c.stdin != nil {
		if err := c.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
			return nil, err
		}
		for c.stdout.Scan() {
			var m struct {
				ID     json.RawMessage `json:"id"`
				Result json.RawMessage `json:"result"`
				Error  *struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal(c.stdout.Bytes(), &m) != nil || string(m.ID) != fmt.Sprint(id) {
				continue
			}
			if m.Error != nil {
				return nil, errors.New(m.Error.Message)
			}
			return m.Result, nil
		}
		return nil, fmt.Errorf("no response to %s", method)
	}
	resp, err := c.post(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %s: %s", resp.Status, b)
	}
	if s := resp.Header.Get("Mcp-Session-Id"); s != "" {
		c.session = s
	}
	var result json.RawMessage
	var rpcErr error
	match := func(b []byte) bool {
		var m struct {
			ID     json.RawMessage `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(b, &m) != nil || string(m.ID) != fmt.Sprint(id) {
			return false
		}
		if m.Error != nil {
			rpcErr = errors.New(m.Error.Message)
		}
		result = m.Result
		return true
	}
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		if sseUntil(resp.Body, match) {
			return result, rpcErr
		}
	} else if b, err := io.ReadAll(resp.Body); err != nil {
		return nil, err
	} else if match(b) {
		return result, rpcErr
	}
	return nil, fmt.Errorf("no response to %s", method)
}

func (c *MCP) post(msg any) (*http.Response, error) {
	b, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, c.url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", "2025-06-18")
	if c.session != "" {
		req.Header.Set("Mcp-Session-Id", c.session)
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	return http.DefaultClient.Do(req)
}

// sseUntil reads SSE events until done accepts the data of one; it reports whether one did.
func sseUntil(r io.Reader, done func([]byte) bool) bool {
	var data []byte
	s := bufio.NewScanner(r)
	s.Buffer(nil, 64*1024*1024)
	for s.Scan() {
		line := s.Text()
		switch {
		case line == "":
			if data != nil && done(data) {
				return true
			}
			data = nil
		case strings.HasPrefix(line, "data:"):
			if data != nil {
				data = append(data, '\n')
			}
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " ")...)
		}
	}
	return data != nil && done(data)
}

// Directive is a line of a scenario that is not provider output but tells the fake what to do:
// "ask" for permission for the tool call just printed, "allow" and "deny" start the branch played
// for that answer, "hang" blocks until killed, "resumed" (claude) plays the recording only on a
// resumed session.
func Directive(line []byte) string {
	var d struct {
		Fake string `json:"fake"`
	}
	_ = json.Unmarshal(line, &d)
	return d.Fake
}
