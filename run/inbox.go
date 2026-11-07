package run

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"atoll/store"
)

// post holds text for the thread as a message from the Bot fromBotID (empty for the user), and
// starts a run for it when the thread has none going. Held messages go in order, one run each, as
// runs end; they survive a restart.
func (m *Manager) post(ctx context.Context, threadID, fromBotID, text string) error {
	if err := m.cfg.Store.Enqueue(ctx, threadID, fromBotID, text); err != nil {
		return err
	}
	return m.flush(ctx, threadID)
}

// flush starts a run for the thread's oldest held message. It does nothing when a run is going
// (that run's end flushes again) or Atoll is stopping. A message for a closed or gone thread is
// dropped.
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

// flushLog is flush for a caller that has no error to return.
func (m *Manager) flushLog(threadID string) {
	if err := m.flush(context.Background(), threadID); err != nil {
		m.cfg.Log.Error("deliver held message", "thread_id", threadID, "error", err)
	}
}

// flushBoss delivers what the thread's boss holds, as the wake of a helper run that just ended.
// A thread with no boss has none.
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

// recoverInbox delivers the held messages of every thread, among them the wakes of the helper
// runs the restart failed.
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

// WakeText is the message for the boss of the helper run w. A helper's branch off the boss's
// branch is named for the boss to merge.
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
