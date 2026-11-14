package run

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

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
	return busy, m.drain(ctx, threadID, true)
}

// drain starts runs for the thread's held messages until a run is going or none are held. With
// wait set, held helper messages alone wait for the wake window.
func (m *Manager) drain(ctx context.Context, threadID string, wait bool) error {
	m.inboxMu.Lock()
	defer m.inboxMu.Unlock()
	for m.ctx.Err() == nil {
		items, err := m.pick(ctx, threadID, wait)
		if err != nil || len(items) == 0 {
			return err
		}
		_, _, err = m.sendHeld(ctx, items...)
		switch {
		case err == nil, errors.Is(err, ErrBusy):
			return nil
		case errors.Is(err, store.ErrNoneHeld):
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

// pick returns the held messages the thread's next run takes, none when nothing is held or the
// wake window still holds them. A helper thread's run takes one message, or the person's. Any
// other thread's takes everything held. inboxMu is held.
func (m *Manager) pick(ctx context.Context, threadID string, wait bool) ([]store.InboxItem, error) {
	t, err := m.cfg.Store.Thread(ctx, threadID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	if t.ParentThreadID != "" {
		it, ok, err := m.cfg.Store.OldestInbox(ctx, threadID)
		if err != nil || !ok {
			return nil, err
		}
		if it.FromUser {
			return m.cfg.Store.UserInbox(ctx, threadID)
		}
		return []store.InboxItem{it}, nil
	}
	items, err := m.cfg.Store.Held(ctx, threadID)
	if err != nil || len(items) == 0 || !wait || t.ID == "" || m.cfg.WakeWindow <= 0 {
		return items, err
	}
	for _, it := range items {
		if it.FromUser || it.FromBotID == "" {
			return items, nil
		}
	}
	left := m.cfg.WakeWindow - time.Since(items[0].CreatedAt)
	if left <= 0 {
		return items, nil
	}
	if _, open := m.windows[threadID]; !open {
		m.windows[threadID] = time.AfterFunc(left, func() {
			m.inboxMu.Lock()
			delete(m.windows, threadID)
			m.inboxMu.Unlock()
			m.flushLog(threadID)
		})
	}
	return nil, nil
}

// takeAll adds what else is held for a thread that is not a helper thread to items, the person's
// messages in items first, so one run takes all of it. inboxMu is held.
func (m *Manager) takeAll(ctx context.Context, threadID string, items []store.InboxItem) ([]store.InboxItem, error) {
	t, err := m.cfg.Store.Thread(ctx, threadID)
	if err != nil || t.ParentThreadID != "" {
		return items, err
	}
	return m.cfg.Store.Held(ctx, threadID)
}

// sendWithHeld sends the person's message it as a new run, which also takes every message held for
// a thread that is not a helper thread: the person's first, then the rest.
func (m *Manager) sendWithHeld(ctx context.Context, it store.InboxItem) (store.Message, store.Run, error) {
	m.inboxMu.Lock()
	defer m.inboxMu.Unlock()
	items := []store.InboxItem{it}
	held, err := m.takeAll(ctx, it.ThreadID, nil)
	if err != nil {
		return store.Message{}, store.Run{}, fmt.Errorf("start run: %w", err)
	}
	if len(held) > 0 {
		at := slices.IndexFunc(held, func(h store.InboxItem) bool { return !h.FromUser })
		if at < 0 {
			at = len(held)
		}
		items = slices.Insert(held, at, it)
	}
	return m.send(ctx, items...)
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
	if err := m.drain(context.Background(), threadID, true); err != nil {
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
		if err := m.drain(ctx, id, false); err != nil {
			m.cfg.Log.Error("deliver held message", "thread_id", id, "error", err)
		}
	}
}

// _helperNotice is the kind the prompt gives a helper's wake, which the store keeps with none.
const _helperNotice = "helper"

// asNotices marks the held messages that come from Reef with a kind: a helper's wake in a thread
// that is not a helper's has none stored. It reports whether every message is a notice, which makes
// the run a background run: a helper thread has none.
func asNotices(thread store.Thread, items []store.InboxItem) ([]store.InboxItem, bool) {
	out := slices.Clone(items)
	all := thread.ParentThreadID == ""
	for i := range out {
		if thread.ParentThreadID == "" && out[i].Kind == "" && out[i].FromBotID != "" {
			out[i].Kind = _helperNotice
		}
		all = all && out[i].Kind != ""
	}
	return out, all
}

// WakeText is the message that tells a boss its helper's run ended; helpers is the helpers line.
func WakeText(w store.Wake, helpers string) string {
	helper, threadID, run := w.Helper, w.ThreadID, w.Run
	merge := ""
	if w.BossBranch != "" && strings.HasPrefix(w.Branch, w.BossBranch+"--") {
		merge = w.Branch
	}
	switch run.Status {
	case store.StatusDone:
		var b strings.Builder
		fmt.Fprintf(&b, "%s finished thread %s:\n%s\n", helper, threadID, run.Reply)
		if merge != "" {
			fmt.Fprintf(&b, "\nMerge %s into your branch.", merge)
		}
		return strings.TrimSpace(b.String()) + "\n\n" + helpers
	case store.StatusStopped:
		return fmt.Sprintf("The user stopped %s\n(thread %s)\n\n%s", helper, threadID, helpers)
	}
	if run.ErrorKind == store.ErrorRateLimited {
		until := "its Claude limit resets"
		if run.Provider == "codex" {
			until = "its Codex limit resets"
		}
		if run.ResetsAt != "" {
			until = run.ResetsAt
		}
		return fmt.Sprintf("%s is out until %s.\nDo not send it work until then.\n(thread %s)\n\n%s", helper, until, threadID, helpers)
	}
	return fmt.Sprintf("%s's run failed: %s\n(thread %s)\n\n%s", helper, run.Error, threadID, helpers)
}

// RestartWakeText is the message that tells a boss the restart interrupted its helpers' runs.
func RestartWakeText(ws []store.Wake) string {
	helpers := HelpersLine(nil)
	if len(ws) == 1 {
		return WakeText(ws[0], helpers)
	}
	parts := []string{fmt.Sprintf("A restart interrupted %d helper runs.", len(ws))}
	for _, w := range ws {
		parts = append(parts, strings.TrimSpace(WakeText(w, "")))
	}
	return strings.Join(append(parts, helpers), "\n\n")
}
