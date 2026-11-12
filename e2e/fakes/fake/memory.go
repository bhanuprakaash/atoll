package fake

import (
	"encoding/json"
	"fmt"
	"strings"
)

// DecisionBody is the durable decision the save scenario writes.
const DecisionBody = "Use SQLite WAL for the journal."

// DecisionWhy is its rationale, carried through MCP and the thread.
const DecisionWhy = "Readers must not block journal writes."

// PairFindingBody and PairDecisionBody are the two items the save-pair scenario writes in one run.
const (
	PairFindingBody  = "The journal fsync takes 40 ms on CI."
	PairDecisionBody = "Batch journal writes every 50 ms."
)

// MemoryItem is only the wire contract exercised by the scripted providers.
type MemoryItem struct {
	ID             string `json:"id"`
	Kind           string `json:"kind"`
	Body           string `json:"body"`
	Why            string `json:"why"`
	Scope          string `json:"scope"`
	ScopeID        string `json:"scope_id"`
	Status         string `json:"status"`
	SourceBotID    string `json:"source_bot_id"`
	SourceThreadID string `json:"source_thread_id"`
	SourceRunID    string `json:"source_run_id"`
	Ticket         string `json:"ticket"`
	RetiredReason  string `json:"retired_reason"`
}

// MemoryStep uses real MCP results for its reply; a canned answer cannot prove recall.
func MemoryStep(c *MCP, mode, prompt string, beforeCall func(string, json.RawMessage) error) (string, error) {
	if mode == "resume" {
		return "Checked SQLite memory.", nil
	}
	if c == nil {
		return "", fmt.Errorf("atoll MCP server absent")
	}
	call := func(tool string, args any, out any) error {
		b, err := json.Marshal(args)
		if err != nil {
			return err
		}
		if beforeCall != nil {
			if err := beforeCall(tool, b); err != nil {
				return err
			}
		}
		text, bad, err := c.Call(tool, b)
		if err != nil {
			return err
		}
		if bad {
			return fmt.Errorf("%s: %s", tool, text)
		}
		if err := json.Unmarshal([]byte(text), out); err != nil {
			return fmt.Errorf("%s result %q: %w", tool, text, err)
		}
		return nil
	}
	if mode == "save-pair" {
		for _, args := range []map[string]any{
			{"kind": "finding", "body": PairFindingBody, "why": "Measured on the CI runners.", "scope": "user"},
			{"kind": "decision", "body": PairDecisionBody, "why": "Keeps fsync off the hot path.", "scope": "user"},
		} {
			var item MemoryItem
			if err := call("remember", args, &item); err != nil {
				return "", err
			}
			if item.ID == "" || item.Status != "accepted" || item.Body != args["body"] {
				return "", fmt.Errorf("remember returned invalid item: %+v", item)
			}
		}
		return "Saved a finding and a decision.", nil
	}
	if mode == "save" || mode == "save-user" {
		scope := "repo"
		if mode == "save-user" {
			scope = "user"
		}
		var item MemoryItem
		if err := call("remember", map[string]any{"kind": "decision", "body": DecisionBody, "why": DecisionWhy, "scope": scope}, &item); err != nil {
			return "", err
		}
		if item.ID == "" || item.Status != "accepted" || item.Body != DecisionBody || item.Why != DecisionWhy || item.Scope != scope {
			return "", fmt.Errorf("remember returned invalid item: %+v", item)
		}
		return "Saved SQLite decision.", nil
	}
	var hits []MemoryItem
	if err := call("recall", map[string]any{"query": "SQLite journal", "limit": 8}, &hits); err != nil {
		return "", err
	}
	for _, hit := range hits {
		if !strings.Contains(hit.Body, "SQLite WAL") || hit.Status != "accepted" {
			continue
		}
		var item MemoryItem
		if err := call("read_memory", map[string]string{"id": hit.ID}, &item); err != nil {
			return "", err
		}
		if item.ID != hit.ID || item.Body != hit.Body || item.Why == "" {
			return "", fmt.Errorf("read_memory disagrees with recall: %+v", item)
		}
		if mode == "retire" {
			var retired MemoryItem
			if err := call("retire_memory", map[string]string{"id": item.ID}, &retired); err != nil {
				return "", err
			}
			if retired.ID != item.ID || retired.Status != "retired" {
				return "", fmt.Errorf("retire_memory returned %+v", retired)
			}
			return "Retired SQLite decision.", nil
		}
		if !strings.Contains(prompt, item.ID) || !strings.Contains(prompt, item.Body) {
			return "", fmt.Errorf("recalled decision missing from new thread's first message")
		}
		return item.Body + " " + item.Why, nil
	}
	return "", fmt.Errorf("recall returned no accepted SQLite journal decision: %+v", hits)
}
