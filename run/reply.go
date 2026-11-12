package run

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"atoll/store"
)

// ErrNoApproval is returned by Reply when no approval is open in the thread.
var ErrNoApproval = errors.New("no approval is open")

// Replied is what Reply did: the stored message, the thread's going run (nil when only a helper's
// approval was answered) and the ids of the approvals it denied.
type Replied struct {
	Message  store.Message
	Run      *store.Run
	Answered []string
}

// Reply stores text as the user's message and denies every approval open in the thread, a
// helper's included, with text as the reason. It starts no run. It returns ErrNoApproval when
// none is open.
func (m *Manager) Reply(ctx context.Context, threadID, text string) (Replied, error) {
	helpers, err := m.cfg.Store.HelperThreads(ctx, threadID)
	if err != nil {
		return Replied{}, fmt.Errorf("list helper threads: %w", err)
	}
	threads := []string{threadID}
	for _, h := range helpers {
		threads = append(threads, h.ID)
	}
	m.mu.Lock()
	taken := map[string]open{}
	for id, o := range m.approvals {
		if slices.Contains(threads, o.run.threadID) {
			taken[id] = o
			delete(m.approvals, id)
		}
	}
	var going string
	if a := m.runs[threadID]; a != nil {
		going = a.run.ID
	}
	m.mu.Unlock()
	if len(taken) == 0 {
		return Replied{}, ErrNoApproval
	}
	ids := slices.Sorted(func(yield func(string) bool) {
		for id := range taken {
			if !yield(id) {
				return
			}
		}
	})
	msg, err := m.storeReply(ctx, threadID, going, text, ids, taken)
	if err != nil {
		m.mu.Lock()
		for id, o := range taken {
			m.approvals[id] = o
		}
		m.mu.Unlock()
		return Replied{}, err
	}
	for _, o := range taken {
		o.answer <- Decision{Reason: text}
	}
	r := Replied{Message: msg, Answered: ids}
	if going != "" {
		ru, err := m.cfg.Store.Run(ctx, going)
		if err != nil {
			m.cfg.Log.Error("read run", "run_id", going, "error", err)
			return r, nil
		}
		r.Run = &ru
	}
	return r, nil
}

// storeReply stores the reply under the going run, or the thread's latest when none goes.
func (m *Manager) storeReply(
	ctx context.Context, threadID, runID, text string, ids []string, taken map[string]open,
) (store.Message, error) {
	if runID == "" {
		var err error
		if runID, err = m.cfg.Store.LatestRun(ctx, threadID); err != nil {
			return store.Message{}, err
		}
	}
	answered := make([]store.AnsweredApproval, len(ids))
	for i, id := range ids {
		ap := taken[id].approval
		answered[i] = store.AnsweredApproval{ID: id, Tool: ap.Tool, Input: ap.Input}
	}
	return m.cfg.Store.AddReply(ctx, threadID, runID, text, answered)
}
