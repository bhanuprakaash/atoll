package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Change entities and actions.
const (
	EntityBot    = "bot"
	EntitySkill  = "skill"
	EntityRepo   = "repo"
	ActionCreate = "create"
	ActionEdit   = "edit"
)

// Skill is a reusable instruction in the library.
type Skill struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

// Change records a platform modification and its source.
type Change struct {
	ID             string          `json:"id"`
	Entity         string          `json:"entity"`
	EntityID       string          `json:"entity_id"`
	Action         string          `json:"action"`
	Before         json.RawMessage `json:"before"`
	After          json.RawMessage `json:"after"`
	Undone         bool            `json:"undone"`
	SourceBotID    string          `json:"source_bot_id"`
	SourceThreadID string          `json:"source_thread_id"`
	SourceRunID    string          `json:"source_run_id"`
	Created        time.Time       `json:"created_at"`
}

func normalizeBot(b Bot) Bot {
	if b.Connections == nil {
		b.Connections = []string{}
	}
	if b.Skills == nil {
		b.Skills = []string{}
	}
	return b
}

// MarshalJSON encodes empty connections and skills as arrays.
func (b Bot) MarshalJSON() ([]byte, error) {
	type botJSON Bot
	return json.Marshal(botJSON(normalizeBot(b)))
}

func botArgs(b Bot) ([]any, error) {
	b = normalizeBot(b)
	var access any
	if b.RepoAccess != nil {
		data, err := json.Marshal([]string(b.RepoAccess))
		if err != nil {
			return nil, fmt.Errorf("encode repo access: %w", err)
		}
		access = string(data)
	}
	conns, err := json.Marshal(b.Connections)
	if err != nil {
		return nil, fmt.Errorf("encode connections: %w", err)
	}
	skills, err := json.Marshal(b.Skills)
	if err != nil {
		return nil, fmt.Errorf("encode skills: %w", err)
	}
	return []any{
		b.ID, b.Name, b.Persona, b.Provider, access, string(conns), string(skills),
		b.CanManagePlatform,
	}, nil
}

// EnsureBot inserts b if its ID has never been stored.
func (s *Store) EnsureBot(ctx context.Context, b Bot) (bool, error) {
	args, err := botArgs(b)
	if err != nil {
		return false, err
	}
	res, err := s.db.ExecContext(ctx,
		"INSERT INTO bots ("+_botColumns+") VALUES (?, ?, ?, ?, ?, ?, ?, ?) "+
			"ON CONFLICT(id) DO NOTHING", args...)
	if err != nil {
		return false, fmt.Errorf("ensure bot: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("count bots: %w", err)
	}
	return n != 0, nil
}

type execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func updateBot(ctx context.Context, db execer, b Bot) error {
	args, err := botArgs(b)
	if err != nil {
		return err
	}
	args = append(args[1:], b.ID)
	res, err := db.ExecContext(ctx,
		"UPDATE bots SET name = ?, persona = ?, provider = ?, repo_access = ?, connections = ?, "+
			"skills = ?, can_manage_platform = ? WHERE id = ? AND removed = 0", args...)
	if err != nil {
		return fmt.Errorf("update bot: %w", err)
	}
	return found(res, "bot", b.ID)
}

// UpdateBot replaces the settings of an existing bot.
func (s *Store) UpdateBot(ctx context.Context, b Bot) (Bot, error) {
	b = normalizeBot(b)
	if err := updateBot(ctx, s.db, b); err != nil {
		return Bot{}, err
	}
	return b, nil
}

// CreateSkill adds a skill to the library.
func (s *Store) CreateSkill(ctx context.Context, name, content string) (Skill, error) {
	sk := Skill{ID: newID(), Name: name, Content: content}
	_, err := s.db.ExecContext(ctx, "INSERT INTO skills (id, name, content) VALUES (?, ?, ?)",
		sk.ID, sk.Name, sk.Content)
	if err != nil {
		return Skill{}, fmt.Errorf("insert skill: %w", err)
	}
	return sk, nil
}

func scanSkill(r *sql.Rows) (sk Skill, err error) {
	return sk, r.Scan(&sk.ID, &sk.Name, &sk.Content)
}

// Skill returns the skill with id.
func (s *Store) Skill(ctx context.Context, id string) (Skill, error) {
	rows, err := query(ctx, s.db,
		"SELECT id, name, content FROM skills WHERE id = ?", scanSkill, id)
	if err != nil {
		return Skill{}, fmt.Errorf("read skill %s: %w", id, err)
	}
	if len(rows) == 0 {
		return Skill{}, fmt.Errorf("skill %s: %w", id, ErrNotFound)
	}
	return rows[0], nil
}

// Skills lists skills in creation order.
func (s *Store) Skills(ctx context.Context) ([]Skill, error) {
	return query(ctx, s.db, "SELECT id, name, content FROM skills ORDER BY rowid", scanSkill)
}

func updateSkill(ctx context.Context, db execer, sk Skill) error {
	res, err := db.ExecContext(ctx, "UPDATE skills SET name = ?, content = ? WHERE id = ?",
		sk.Name, sk.Content, sk.ID)
	if err != nil {
		return fmt.Errorf("update skill: %w", err)
	}
	return found(res, "skill", sk.ID)
}

// UpdateSkill replaces the name and content of a skill.
func (s *Store) UpdateSkill(ctx context.Context, sk Skill) (Skill, error) {
	if err := updateSkill(ctx, s.db, sk); err != nil {
		return Skill{}, err
	}
	return sk, nil
}

const _changeColumns = "id, entity, entity_id, action, before, after, undone, source_bot_id, " +
	"source_thread_id, source_run_id, created"

func scanChange(r *sql.Rows) (Change, error) {
	var c Change
	var before, after string
	var created int64
	err := r.Scan(&c.ID, &c.Entity, &c.EntityID, &c.Action, &before, &after, &c.Undone,
		&c.SourceBotID, &c.SourceThreadID, &c.SourceRunID, &created)
	if err != nil {
		return Change{}, err
	}
	c.Before = json.RawMessage(before)
	c.After = json.RawMessage(after)
	c.Created = time.UnixMicro(created).UTC()
	return c, nil
}

// RecordChange stores a platform change with a new ID and timestamp.
func (s *Store) RecordChange(ctx context.Context, c Change) (Change, error) {
	c.ID = newID()
	c.Created = time.Now().UTC().Truncate(time.Microsecond)
	if len(c.Before) == 0 {
		c.Before = json.RawMessage("null")
	}
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO changes ("+_changeColumns+") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		c.ID, c.Entity, c.EntityID, c.Action, string(c.Before), string(c.After), c.Undone,
		c.SourceBotID, c.SourceThreadID, c.SourceRunID, c.Created.UnixMicro())
	if err != nil {
		return Change{}, fmt.Errorf("insert change: %w", err)
	}
	return c, nil
}

// Changes lists a thread’s changes in creation order.
func (s *Store) Changes(ctx context.Context, threadID string) ([]Change, error) {
	return query(ctx, s.db,
		"SELECT "+_changeColumns+" FROM changes WHERE source_thread_id = ? ORDER BY rowid",
		scanChange, threadID)
}

// UndoChange restores the state before a change and marks it undone.
func (s *Store) UndoChange(ctx context.Context, id string) (Change, error) {
	var c Change
	err := s.tx(ctx, func(tx *sql.Tx) error {
		rows, err := query(ctx, tx,
			"SELECT "+_changeColumns+" FROM changes WHERE id = ?", scanChange, id)
		if err != nil {
			return fmt.Errorf("read change %s: %w", id, err)
		}
		if len(rows) == 0 {
			return fmt.Errorf("change %s: %w", id, ErrNotFound)
		}
		c = rows[0]
		if c.Undone {
			return fmt.Errorf("change %s: %w", id, ErrUndone)
		}
		switch {
		case c.Action == ActionCreate:
			var stmt string
			switch c.Entity {
			case EntityBot:
				stmt = "UPDATE bots SET removed = 1 WHERE id = ? AND removed = 0"
			case EntitySkill:
				stmt = "DELETE FROM skills WHERE id = ?"
			case EntityRepo:
				var threads int
				err := tx.QueryRowContext(ctx,
					"SELECT count(*) FROM threads WHERE repo_id = ?", c.EntityID).Scan(&threads)
				if err != nil {
					return fmt.Errorf("count repo threads: %w", err)
				}
				if threads > 0 {
					return fmt.Errorf("repo %s: %w", c.EntityID, ErrInUse)
				}
				stmt = "DELETE FROM repos WHERE id = ?"
			default:
				return fmt.Errorf("unknown change entity %q", c.Entity)
			}
			res, err := tx.ExecContext(ctx, stmt, c.EntityID)
			if err != nil {
				return fmt.Errorf("undo create: %w", err)
			}
			if err := found(res, c.Entity, c.EntityID); err != nil {
				return err
			}
		case c.Action == ActionEdit && c.Entity == EntityBot:
			var b Bot
			if err := json.Unmarshal(c.Before, &b); err != nil {
				return fmt.Errorf("decode bot before: %w", err)
			}
			b.ID = c.EntityID
			if err := updateBot(ctx, tx, b); err != nil {
				return err
			}
		case c.Action == ActionEdit && c.Entity == EntitySkill:
			var sk Skill
			if err := json.Unmarshal(c.Before, &sk); err != nil {
				return fmt.Errorf("decode skill before: %w", err)
			}
			sk.ID = c.EntityID
			if err := updateSkill(ctx, tx, sk); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown change action %q for %q", c.Action, c.Entity)
		}
		_, err = tx.ExecContext(ctx, "UPDATE changes SET undone = 1 WHERE id = ?", id)
		if err != nil {
			return fmt.Errorf("mark change undone: %w", err)
		}
		c.Undone = true
		return nil
	})
	if err != nil {
		return Change{}, err
	}
	return c, nil
}
