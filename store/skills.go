package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
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

// Skill is a reusable instruction in the library: SKILL.md as Content, plus its other text files.
// Source is the folder it was imported from, or "". Imported is the hash of its files as last
// imported, so an edit made in Reef since then shows as a mismatch.
type Skill struct {
	ID       string      `json:"id"`
	Name     string      `json:"name"`
	Content  string      `json:"content"`
	Source   string      `json:"source"`
	Files    []SkillFile `json:"files"`
	Imported string      `json:"-"`
}

// SkillFile is one file of a skill besides SKILL.md, at a slash-separated path inside its folder.
type SkillFile struct {
	Path       string `json:"path"`
	Content    string `json:"content"`
	Executable bool   `json:"executable"`
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
	if b.MCPServers == nil {
		b.MCPServers = []string{}
	}
	if b.Delegates == nil {
		b.Delegates = []string{}
	}
	if b.Mode == "" {
		b.Mode = "auto"
	}
	return b
}

// MarshalJSON encodes empty connections, skills, MCP servers and delegates as arrays.
func (b Bot) MarshalJSON() ([]byte, error) {
	type botJSON Bot
	return json.Marshal(botJSON(normalizeBot(b)))
}

// MarshalJSON encodes empty files as an array.
func (sk Skill) MarshalJSON() ([]byte, error) {
	type skillJSON Skill
	if sk.Files == nil {
		sk.Files = []SkillFile{}
	}
	return json.Marshal(skillJSON(sk))
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
	mcp, err := json.Marshal(b.MCPServers)
	if err != nil {
		return nil, fmt.Errorf("encode MCP servers: %w", err)
	}
	delegates, err := json.Marshal(b.Delegates)
	if err != nil {
		return nil, fmt.Errorf("encode delegates: %w", err)
	}
	return []any{
		b.ID, b.Name, b.Persona, b.Provider, access, string(conns), string(skills),
		b.CanManagePlatform, b.Look, b.Model, string(mcp), b.Mode, string(delegates),
	}, nil
}

// EnsureBot inserts b if its ID has never been stored.
func (s *Store) EnsureBot(ctx context.Context, b Bot) (bool, error) {
	args, err := botArgs(b)
	if err != nil {
		return false, err
	}
	res, err := s.db.ExecContext(ctx,
		"INSERT INTO bots ("+_botColumns+") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) "+
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
			"skills = ?, can_manage_platform = ?, look = ?, model = ?, mcp_servers = ?, mode = ?, delegates = ? "+
			"WHERE id = ? AND removed = 0", args...)
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

// CreateSkill adds a skill and its files to the library.
func (s *Store) CreateSkill(ctx context.Context, sk Skill) (Skill, error) {
	sk.ID = newID()
	err := s.tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			"INSERT INTO skills (id, name, content, source, imported) VALUES (?, ?, ?, ?, ?)",
			sk.ID, sk.Name, sk.Content, sk.Source, sk.Imported)
		if err != nil {
			return fmt.Errorf("insert skill: %w", err)
		}
		return insertSkillFiles(ctx, tx, sk)
	})
	if err != nil {
		return Skill{}, err
	}
	return s.Skill(ctx, sk.ID)
}

const _skillColumns = "id, name, content, source, imported"

func scanSkill(r *sql.Rows) (sk Skill, err error) {
	return sk, r.Scan(&sk.ID, &sk.Name, &sk.Content, &sk.Source, &sk.Imported)
}

type skillFileRow struct {
	skillID string
	file    SkillFile
}

func scanSkillFile(r *sql.Rows) (f skillFileRow, err error) {
	return f, r.Scan(&f.skillID, &f.file.Path, &f.file.Content, &f.file.Executable)
}

// Skill returns the skill with id and its files.
func (s *Store) Skill(ctx context.Context, id string) (Skill, error) {
	skills, err := s.skills(ctx, id)
	if err != nil {
		return Skill{}, fmt.Errorf("read skill %s: %w", id, err)
	}
	if len(skills) == 0 {
		return Skill{}, fmt.Errorf("skill %s: %w", id, ErrNotFound)
	}
	return skills[0], nil
}

// Skills lists skills with their files in creation order.
func (s *Store) Skills(ctx context.Context) ([]Skill, error) {
	return s.skills(ctx, "")
}

// skills reads the skill with id, or every skill when id is "".
func (s *Store) skills(ctx context.Context, id string) ([]Skill, error) {
	skillsWhere, filesWhere, args := "", "", []any{}
	if id != "" {
		skillsWhere, filesWhere, args = " WHERE id = ?", " WHERE skill_id = ?", []any{id}
	}
	skills, err := query(ctx, s.db,
		"SELECT "+_skillColumns+" FROM skills"+skillsWhere+" ORDER BY rowid", scanSkill, args...)
	if err != nil || len(skills) == 0 {
		return skills, err
	}
	files, err := query(ctx, s.db,
		"SELECT skill_id, path, content, executable FROM skill_files"+filesWhere+" ORDER BY path",
		scanSkillFile, args...)
	if err != nil {
		return nil, fmt.Errorf("read skill files: %w", err)
	}
	byID := make(map[string]int, len(skills))
	for i, sk := range skills {
		byID[sk.ID] = i
	}
	for _, f := range files {
		i := byID[f.skillID]
		skills[i].Files = append(skills[i].Files, f.file)
	}
	return skills, nil
}

func insertSkillFiles(ctx context.Context, db execer, sk Skill) error {
	for _, f := range sk.Files {
		_, err := db.ExecContext(ctx,
			"INSERT INTO skill_files (skill_id, path, content, executable) VALUES (?, ?, ?, ?)",
			sk.ID, f.Path, f.Content, f.Executable)
		if err != nil {
			return fmt.Errorf("insert skill file %s: %w", f.Path, err)
		}
	}
	return nil
}

// updateSkill replaces the name, content and whole file set of a skill. Source and Imported change
// only when Imported is set, which an import does and an edit does not.
func updateSkill(ctx context.Context, db execer, sk Skill) error {
	stmt, args := "UPDATE skills SET name = ?, content = ?", []any{sk.Name, sk.Content}
	if sk.Imported != "" {
		stmt, args = stmt+", source = ?, imported = ?", append(args, sk.Source, sk.Imported)
	}
	res, err := db.ExecContext(ctx, stmt+" WHERE id = ?", append(args, sk.ID)...)
	if err != nil {
		return fmt.Errorf("update skill: %w", err)
	}
	if err := found(res, "skill", sk.ID); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM skill_files WHERE skill_id = ?", sk.ID); err != nil {
		return fmt.Errorf("clear skill files: %w", err)
	}
	return insertSkillFiles(ctx, db, sk)
}

// UpdateSkill replaces the name, content and files of a skill in one transaction.
func (s *Store) UpdateSkill(ctx context.Context, sk Skill) (Skill, error) {
	err := s.tx(ctx, func(tx *sql.Tx) error { return updateSkill(ctx, tx, sk) })
	if err != nil {
		return Skill{}, err
	}
	return s.Skill(ctx, sk.ID)
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

// ChangesSince lists the platform changes made after since, oldest first.
func (s *Store) ChangesSince(ctx context.Context, since time.Time) ([]Change, error) {
	return query(ctx, s.db,
		"SELECT "+_changeColumns+" FROM changes WHERE created > ? ORDER BY rowid",
		scanChange, since.UnixMicro())
}

// Change returns one recorded platform change.
func (s *Store) Change(ctx context.Context, id string) (Change, error) {
	rows, err := query(ctx, s.db,
		"SELECT "+_changeColumns+" FROM changes WHERE id = ?", scanChange, id)
	if err != nil {
		return Change{}, fmt.Errorf("read change %s: %w", id, err)
	}
	if len(rows) == 0 {
		return Change{}, fmt.Errorf("change %s: %w", id, ErrNotFound)
	}
	return rows[0], nil
}

// UndoChange restores the state before a change and marks it undone. mcpServers are the MCP
// server ids a Bot may pick, catalog ones included; a restored Bot keeps only those.
func (s *Store) UndoChange(ctx context.Context, id string, mcpServers []string) (Change, error) {
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
			// The HTTP undo handler removes Bots through the run manager first.
			if c.Entity != EntityBot {
				if err := found(res, c.Entity, c.EntityID); err != nil {
					return err
				}
			}
			if c.Entity == EntityBot {
				if err := dropDelegate(ctx, tx, c.EntityID); err != nil {
					return err
				}
			}
			if c.Entity == EntityRepo {
				_, err := tx.ExecContext(ctx,
					"UPDATE bots SET repo_access = (SELECT json_group_array(value) FROM json_each(repo_access) "+
						"WHERE value != ?1) WHERE EXISTS (SELECT 1 FROM json_each(repo_access) WHERE value = ?1)",
					c.EntityID)
				if err != nil {
					return fmt.Errorf("drop repo from bot access: %w", err)
				}
			}
		case c.Action == ActionEdit && c.Entity == EntityBot:
			var b Bot
			if err := json.Unmarshal(c.Before, &b); err != nil {
				return fmt.Errorf("decode bot before: %w", err)
			}
			b.ID = c.EntityID
			b.MCPServers = slices.DeleteFunc(b.MCPServers, func(id string) bool {
				return !slices.Contains(mcpServers, id)
			})
			if err := filterBotIDs(ctx, tx, &b); err != nil {
				return err
			}
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

func filterBotIDs(ctx context.Context, tx *sql.Tx, b *Bot) error {
	for _, refs := range []struct {
		ids   *[]string
		table string
	}{
		{(*[]string)(&b.RepoAccess), "repos"},
		{&b.Delegates, "bots WHERE removed = 0"},
		{&b.Skills, "skills"},
		{&b.Connections, "connections"},
	} {
		if *refs.ids == nil {
			continue
		}
		data, err := json.Marshal(*refs.ids)
		if err != nil {
			return fmt.Errorf("encode bot ids: %w", err)
		}
		ids, err := query(ctx, tx,
			"SELECT value FROM json_each(?) WHERE value IN (SELECT id FROM "+refs.table+")",
			func(r *sql.Rows) (string, error) {
				var id string
				return id, r.Scan(&id)
			}, string(data))
		if err != nil {
			return fmt.Errorf("filter bot ids: %w", err)
		}
		*refs.ids = append([]string{}, ids...)
	}
	return nil
}
