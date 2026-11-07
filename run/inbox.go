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

// recoverInbox wakes the boss of every helper run that was failed at start, then delivers the held
// messages of every thread.
func (m *Manager) recoverInbox(failed []string) {
	for _, id := range failed {
		m.wake(id)
	}
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

// wake tells the boss of a helper thread that its run runID ended, as a message from the helper's
// Bot. A run of any other thread has no boss to tell.
func (m *Manager) wake(runID string) {
	ctx := context.Background()
	run, err := m.cfg.Store.Run(ctx, runID)
	var t store.Thread
	if err == nil {
		t, err = m.cfg.Store.Thread(ctx, run.ThreadID)
	}
	var bot store.Bot
	if err == nil && t.ParentThreadID != "" {
		bot, err = m.cfg.Store.Bot(ctx, t.BotID)
	}
	if errors.Is(err, store.ErrNotFound) || err == nil && t.ParentThreadID == "" {
		return
	}
	var boss store.Thread
	if err == nil {
		boss, err = m.cfg.Store.Thread(ctx, t.ParentThreadID)
	}
	if err == nil {
		merge := ""
		if boss.Branch != "" && strings.HasPrefix(t.Branch, boss.Branch+"--") {
			merge = t.Branch
		}
		err = m.post(ctx, t.ParentThreadID, bot.ID, wakeText(bot.Name, t.ID, merge, run))
	}
	if err != nil {
		m.cfg.Log.Error("wake boss", "run_id", runID, "error", err)
	}
}

// wakeText is the message for the boss. merge is the helper's branch off the boss's branch, if it
// has one.
func wakeText(helper, threadID, merge string, run store.Run) string {
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
