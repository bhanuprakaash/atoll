package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Enqueue holds the item until a run can start. It returns the item with its ID and its images'.
func (s *Store) Enqueue(ctx context.Context, it InboxItem) (InboxItem, error) {
	it.ID = newID()
	images := it.Images
	it.Images = nil
	it.CreatedAt = time.Now().UTC()
	err := s.tx(ctx, func(tx *sql.Tx) error {
		now := it.CreatedAt.UnixMicro()
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO inbox (id, thread_id, from_bot_id, text, kind, created, queued, from_user)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			it.ID, it.ThreadID, it.FromBotID, it.Text, it.Kind, now, it.Queued, it.FromUser); err != nil {
			return fmt.Errorf("enqueue message: %w", err)
		}
		for _, img := range images {
			img.ID = newID()
			if _, err := tx.ExecContext(ctx,
				"INSERT INTO inbox_images (id, inbox_id, mime, data, created_at) VALUES (?, ?, ?, ?, ?)",
				img.ID, it.ID, img.Mime, img.Data, now); err != nil {
				return fmt.Errorf("enqueue image: %w", err)
			}
			it.Images = append(it.Images, img)
		}
		return nil
	})
	if err != nil {
		return InboxItem{}, err
	}
	return it, nil
}

// Queued lists the thread's held messages that show in its queue, oldest first, images without
// their data.
func (s *Store) Queued(ctx context.Context, threadID string) ([]InboxItem, error) {
	items, err := s.inbox(ctx, "WHERE thread_id = ? AND queued = 1 ORDER BY rowid", threadID)
	for i := range items {
		for j := range items[i].Images {
			items[i].Images[j].Data = nil
		}
	}
	return items, err
}

// UserInbox lists the messages the person sent to the thread that wait for a run, oldest first.
func (s *Store) UserInbox(ctx context.Context, threadID string) ([]InboxItem, error) {
	return s.inbox(ctx, "WHERE thread_id = ? AND from_user = 1 ORDER BY rowid", threadID)
}

// Held lists every message held for the thread in the order one run takes them: the person's
// first, then the rest as they arrived. Images come with their data.
func (s *Store) Held(ctx context.Context, threadID string) ([]InboxItem, error) {
	return s.inbox(ctx, "WHERE thread_id = ? ORDER BY from_user DESC, rowid", threadID)
}

// EditQueued sets the text of a queued message. It wraps ErrNotFound for an id the thread has
// never had queued, returns ErrSent once the message is delivered and ErrEmpty when the result
// would have neither text nor an image.
func (s *Store) EditQueued(ctx context.Context, threadID, id, text string) (InboxItem, error) {
	err := s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			"UPDATE inbox SET text = ? WHERE id = ? AND thread_id = ? AND queued = 1", text, id, threadID)
		if err != nil {
			return fmt.Errorf("edit queued message: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return s.missingQueued(ctx, tx, threadID, id)
		}
		var images int
		if err := tx.QueryRowContext(ctx,
			"SELECT count(*) FROM inbox_images WHERE inbox_id = ?", id).Scan(&images); err != nil {
			return fmt.Errorf("count images: %w", err)
		}
		if text == "" && images == 0 {
			return ErrEmpty
		}
		return nil
	})
	if err != nil {
		return InboxItem{}, err
	}
	items, err := s.Queued(ctx, threadID)
	if err != nil {
		return InboxItem{}, err
	}
	for _, it := range items {
		if it.ID == id {
			return it, nil
		}
	}
	return InboxItem{}, ErrSent
}

// DeleteQueued removes a queued message, with the errors of EditQueued.
func (s *Store) DeleteQueued(ctx context.Context, threadID, id string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			"DELETE FROM inbox WHERE id = ? AND thread_id = ? AND queued = 1", id, threadID)
		if err != nil {
			return fmt.Errorf("delete queued message: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return s.missingQueued(ctx, tx, threadID, id)
		}
		return nil
	})
}

// CheckQueued returns nil when the thread has the message queued, else the errors of EditQueued.
func (s *Store) CheckQueued(ctx context.Context, threadID, id string) error {
	var n int
	err := s.db.QueryRowContext(ctx,
		"SELECT count(*) FROM inbox WHERE id = ? AND thread_id = ? AND queued = 1", id, threadID).Scan(&n)
	if err != nil {
		return fmt.Errorf("find queued message: %w", err)
	}
	if n == 1 {
		return nil
	}
	return s.missingQueued(ctx, s.db, threadID, id)
}

func (s *Store) missingQueued(ctx context.Context, db dbConn, threadID, id string) error {
	err := db.QueryRowContext(ctx,
		"SELECT 1 FROM messages WHERE id = ? AND thread_id = ?", id, threadID).Scan(new(int))
	switch {
	case err == nil:
		return ErrSent
	case errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("message %s: %w", id, ErrNotFound)
	}
	return fmt.Errorf("find message: %w", err)
}

// inbox reads the held messages that match where, with their images.
func (s *Store) inbox(ctx context.Context, where string, args ...any) ([]InboxItem, error) {
	items, err := query(ctx, s.db,
		"SELECT id, thread_id, from_bot_id, text, kind, queued, from_user, created FROM inbox "+where,
		func(r *sql.Rows) (it InboxItem, err error) {
			var created int64
			err = r.Scan(&it.ID, &it.ThreadID, &it.FromBotID, &it.Text, &it.Kind, &it.Queued, &it.FromUser, &created)
			it.CreatedAt = time.UnixMicro(created).UTC()
			return it, err
		}, args...)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].Images, err = query(ctx, s.db,
			"SELECT id, mime, data FROM inbox_images WHERE inbox_id = ? ORDER BY rowid",
			func(r *sql.Rows) (a Attachment, err error) { return a, r.Scan(&a.ID, &a.Mime, &a.Data) },
			items[i].ID)
		if err != nil {
			return nil, fmt.Errorf("list queued images: %w", err)
		}
	}
	return items, nil
}
