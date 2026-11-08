package run

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"atoll/provider"
	"atoll/store"
)

// _titlePrompt is the system prompt of a title session.
const _titlePrompt = "You name chat threads. Reply with only a title for the conversation below: " +
	"at most six words, sentence case, no quotes, no trailing punctuation."

const (
	_titleTimeout = time.Minute
	_titleInput   = 2000 // runes of the message and of the reply the title session sees
)

// _titleModels is each provider's cheapest model.
var _titleModels = map[string]string{"claude": "haiku", "codex": "gpt-6-luna"}

// nameThread replaces a thread's first-words title with one the provider's cheapest model writes
// after the thread's first reply. It runs a session of its own: never the thread's, never a run.
func (m *Manager) nameThread(threadID, provider string) {
	ctx, cancel := context.WithTimeout(m.ctx, _titleTimeout)
	defer cancel()
	t, err := m.cfg.Store.Thread(ctx, threadID)
	if err != nil {
		m.cfg.Log.Warn("name thread", "thread_id", threadID, "error", err)
		return
	}
	msgs, err := m.cfg.Store.Messages(ctx, threadID)
	if err != nil {
		m.cfg.Log.Warn("name thread", "thread_id", threadID, "error", err)
		return
	}
	var first, reply string
	replies := 0
	for _, msg := range msgs {
		switch {
		case msg.Role == store.RoleUser && first == "":
			first = msg.Text
		case msg.Role == store.RoleBot:
			replies++
			reply = msg.Text
		}
	}
	if t.ParentThreadID != "" {
		first = helperBrief(first)
	}
	fallback := store.FirstWords(first)
	if replies != 1 || t.Title != fallback {
		return
	}
	title, err := m.writeTitle(ctx, provider, first, reply)
	if err != nil {
		m.cfg.Log.Warn("name thread", "thread_id", threadID, "error", err)
		return
	}
	if err := m.cfg.Store.ReplaceTitle(ctx, threadID, fallback, title); err != nil {
		m.cfg.Log.Warn("name thread", "thread_id", threadID, "error", err)
	}
}

func (m *Manager) writeTitle(ctx context.Context, name, message, reply string) (string, error) {
	model, ok := _titleModels[name]
	if !ok {
		return "", fmt.Errorf("no title model for %s", name)
	}
	sess, cfg, err := m.cfg.Launch(ctx, name)
	if err != nil {
		return "", fmt.Errorf("launch %s: %w", name, err)
	}
	defer func() { _ = sess.Interrupt() }()
	cfg.Model, cfg.SystemPrompt = model, _titlePrompt
	cfg.NoTools = true
	if cfg.Env, _, err = m.runEnv(ctx, store.Bot{}, false); err != nil {
		return "", err
	}
	cfg.Cwd = filepath.Join(m.cfg.DataDir, "titles")
	if err := os.MkdirAll(cfg.Cwd, 0o700); err != nil {
		return "", fmt.Errorf("create title folder: %w", err)
	}
	if err := sess.Start(ctx, cfg); err != nil {
		return "", fmt.Errorf("start %s: %w", name, err)
	}
	prompt := "Message:\n" + clip(message) + "\n\nReply:\n" + clip(reply)
	if err := sess.Send(ctx, prompt, nil); err != nil {
		return "", fmt.Errorf("send prompt: %w", err)
	}
	var b strings.Builder
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case ev, ok := <-sess.Events():
			if !ok {
				return "", errors.New("provider exited before the title")
			}
			switch ev.Kind {
			case provider.KindText:
				b.WriteString(ev.Text)
			case provider.KindError:
				return "", errors.New(ev.Text)
			case provider.KindTurnEnd:
				if title := cleanTitle(b.String()); title != "" {
					return title, nil
				}
				return "", errors.New("empty title")
			}
		}
	}
}

// cleanTitle is the first line of s as a title: at most six words, no quotes or trailing
// punctuation, first letter upper case.
func cleanTitle(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	s = strings.Trim(s, " \t#*\"'`")
	words := strings.Fields(s)
	if len(words) == 0 {
		return ""
	}
	words = words[:min(6, len(words))]
	words[len(words)-1] = strings.TrimRight(words[len(words)-1], ".,;:!?\"'`")
	s = strings.TrimSpace(strings.Join(words, " "))
	if s == "" {
		return ""
	}
	r, size := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[size:]
}

func clip(s string) string {
	if r := []rune(s); len(r) > _titleInput {
		return string(r[:_titleInput])
	}
	return s
}
