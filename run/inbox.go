package run

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"atoll/store"
)

func (m *Manager) post(ctx context.Context, threadID, fromBotID, text string) error {
	if err := m.cfg.Store.Enqueue(ctx, threadID, fromBotID, text); err != nil {
		return err
	}
	return m.flush(ctx, threadID)
}

func (m *Manager) flush(ctx context.Context, threadID string) error {
	m.inboxMu.Lock()
	defer m.inboxMu.Unlock()
	for m.ctx.Err() == nil {
		it, ok, err := m.cfg.Store.OldestInbox(ctx, threadID)
		if err != nil || !ok {
			return err
		}
		_, _, err = m.send(ctx, it)
		switch {
		case err == nil, errors.Is(err, ErrBusy):
			return nil
		case errors.Is(err, ErrClosed), errors.Is(err, store.ErrNotFound):
			m.cfg.Log.Warn("drop message for closed thread", "thread_id", threadID, "error", err)
			if err := m.cfg.Store.DeleteInbox(ctx, it.ID); err != nil {
				return err
			}
		default:
			return fmt.Errorf("deliver message: %w", err)
		}
	}
	return nil
}

func (m *Manager) flushLog(threadID string) {
	if err := m.flush(context.Background(), threadID); err != nil {
		m.cfg.Log.Error("deliver held message", "thread_id", threadID, "error", err)
	}
}

func (m *Manager) flushBoss(threadID string) {
	t, err := m.cfg.Store.Thread(context.Background(), threadID)
	switch {
	case errors.Is(err, store.ErrNotFound), err == nil && t.ParentThreadID == "":
	case err != nil:
		m.cfg.Log.Error("wake boss", "thread_id", threadID, "error", err)
	default:
		m.flushLog(t.ParentThreadID)
	}
}

func (m *Manager) recoverInbox() {
	ctx := context.Background()
	ids, err := m.cfg.Store.InboxThreads(ctx)
	if err != nil {
		m.cfg.Log.Error("list held messages", "error", err)
		return
	}
	for _, id := range ids {
		if err := m.flush(ctx, id); err != nil {
			m.cfg.Log.Error("deliver held message", "thread_id", id, "error", err)
		}
	}
}

// WakeText is the message that tells a boss its helper's run ended.
func WakeText(w store.Wake) string {
	helper, threadID, run := w.Helper, w.ThreadID, w.Run
	merge := ""
	if w.BossBranch != "" && strings.HasPrefix(w.Branch, w.BossBranch+"--") {
		merge = w.Branch
	}
	switch run.Status {
	case store.StatusDone:
		var b strings.Builder
		fmt.Fprintf(&b, "%s reported back from thread %s.\n%s\n", helper, threadID, run.Reply)
		if r := run.Report; r != nil {
			for _, f := range []struct{ name, text string }{
				{"Done", r.Done}, {"Tried", r.Tried}, {"Blocked", r.Blocked}, {"Next", r.Next},
			} {
				if f.text != "" {
					fmt.Fprintf(&b, "\n%s: %s", f.name, f.text)
				}
			}
		}
		if merge != "" {
			fmt.Fprintf(&b, "\nMerge %s into your branch.", merge)
		}
		return strings.TrimSpace(b.String())
	case "stopped":
		return fmt.Sprintf("The user stopped %s\n(thread %s)", helper, threadID)
	}
	return fmt.Sprintf("%s's run failed: %s\n(thread %s)", helper, run.Error, threadID)
}
