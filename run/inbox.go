package run

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"atoll/store"
)

// post holds a boss's message for a helper thread and starts it when the thread can run. busy says
// the thread's run was going, so the message waits for it.
func (m *Manager) post(ctx context.Context, threadID, text string) (busy bool, err error) {
	m.mu.Lock()
	_, busy = m.runs[threadID]
	m.mu.Unlock()
	if _, err := m.cfg.Store.Enqueue(ctx, store.InboxItem{ThreadID: threadID, Text: text, Queued: true}); err != nil {
		return false, err
	}
	if busy {
		m.cfg.Log.Info("queued", "thread_id", threadID)
	}
	return busy, m.flush(ctx, threadID)
}

func (m *Manager) flush(ctx context.Context, threadID string) error {
	m.inboxMu.Lock()
	defer m.inboxMu.Unlock()
	for m.ctx.Err() == nil {
		it, ok, err := m.cfg.Store.OldestInbox(ctx, threadID)
		if err != nil || !ok {
			return err
		}
		items := []store.InboxItem{it}
		if it.FromUser {
			if items, err = m.cfg.Store.UserInbox(ctx, threadID); err != nil {
				return err
			}
		}
		_, _, err = m.sendHeld(ctx, items...)
		switch {
		case err == nil, errors.Is(err, ErrBusy):
			return nil
		case errors.Is(err, ErrClosed), errors.Is(err, store.ErrNotFound):
			m.cfg.Log.Warn("drop message for closed thread", "thread_id", threadID, "error", err)
			for _, it := range items {
				if err := m.cfg.Store.DeleteInbox(ctx, it.ID); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("deliver message: %w", err)
		}
	}
	return nil
}

// sendHeld starts the one run for held messages of a thread. A message for a helper thread stays
// held while its boss has _maxHelpers helper runs going: it returns ErrBusy.
func (m *Manager) sendHeld(ctx context.Context, items ...store.InboxItem) (store.Message, store.Run, error) {
	t, err := m.cfg.Store.Thread(ctx, items[0].ThreadID)
	if err != nil {
		return store.Message{}, store.Run{}, fmt.Errorf("start run: %w", err)
	}
	if t.ParentThreadID == "" {
		return m.send(ctx, items...)
	}
	m.delegateMu.Lock()
	defer m.delegateMu.Unlock()
	going, err := m.helpersGoing(ctx, t.ParentThreadID)
	if err != nil {
		return store.Message{}, store.Run{}, err
	}
	if going >= _maxHelpers {
		return store.Message{}, store.Run{}, ErrBusy
	}
	return m.send(ctx, items...)
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
		m.flushHelpers(t.ParentThreadID, threadID)
	}
}

// flushHelpers delivers the messages held for the helper threads of bossID, except skip: a slot
// just freed.
func (m *Manager) flushHelpers(bossID, skip string) {
	helpers, err := m.cfg.Store.HelperThreads(context.Background(), bossID)
	if err != nil {
		m.cfg.Log.Error("list helper threads", "thread_id", bossID, "error", err)
		return
	}
	for _, h := range helpers {
		if h.ID != skip {
			m.flushLog(h.ID)
		}
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
	case store.StatusStopped:
		return fmt.Sprintf("The user stopped %s\n(thread %s)", helper, threadID)
	}
	if run.ErrorKind == store.ErrorRateLimited {
		until := "its Claude limit resets"
		if run.Provider == "codex" {
			until = "its Codex limit resets"
		}
		if run.ResetsAt != "" {
			until = run.ResetsAt
		}
		return fmt.Sprintf("%s is out until %s.\nDo not send it work until then.\n(thread %s)", helper, until, threadID)
	}
	return fmt.Sprintf("%s's run failed: %s\n(thread %s)", helper, run.Error, threadID)
}
