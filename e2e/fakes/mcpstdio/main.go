// Command mcpstdio is a local MCP server for the e2e tests: JSON-RPC lines on stdin and stdout, as
// a provider CLI starts one from its config. Tool answer always answers "stdio-answer-42". Tool
// inspect, given {"env": "<name>"}, answers "args=<its arguments joined by |>;env=<that variable>"
// ("(unset)" when it has none), so a test sees what the provider passed. Every start is logged to
// <executable>.log with its arguments, never its env.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type request struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params struct {
		Name      string `json:"name"`
		Arguments struct {
			Env string `json:"env"`
		} `json:"arguments"`
	} `json:"params"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "mcpstdio:", err)
		os.Exit(1)
	}
}

func run() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	f, err := os.OpenFile(exe+".log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	b, _ := json.Marshal(map[string]any{"pid": os.Getpid(), "argv": os.Args[1:]})
	_, err = f.Write(append(b, '\n'))
	_ = f.Close()
	if err != nil {
		return err
	}
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(nil, 64*1024*1024)
	for in.Scan() {
		var req request
		if json.Unmarshal(in.Bytes(), &req) != nil || len(req.ID) == 0 {
			continue
		}
		var result any = map[string]any{}
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}},
				"serverInfo": map[string]string{"name": "mcpstdio", "version": "0"}}
		case "tools/list":
			result = map[string]any{"tools": []any{
				map[string]any{"name": "answer", "description": "Answers a fixed text.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}},
				map[string]any{"name": "inspect", "description": "Reports its arguments and one environment variable.", "inputSchema": map[string]any{
					"type": "object", "properties": map[string]any{"env": map[string]string{"type": "string"}}}},
			}}
		case "tools/call":
			text := "stdio-answer-42"
			if req.Params.Name == "inspect" {
				value, ok := os.LookupEnv(req.Params.Arguments.Env)
				if !ok {
					value = "(unset)"
				}
				text = "args=" + strings.Join(os.Args[1:], "|") + ";env=" + value
			}
			result = map[string]any{"content": []map[string]string{{"type": "text", "text": text}}}
		}
		out, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
		if _, err := fmt.Fprintf(os.Stdout, "%s\n", out); err != nil {
			return err
		}
	}
	return in.Err()
}
