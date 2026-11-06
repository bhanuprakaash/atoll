package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// MCP server kinds.
const (
	MCPCatalog = "catalog"
	MCPLocal   = "local"
	MCPRemote  = "remote"
)

// MCPServer is an MCP server of the library. A local one starts Command with Args; a remote one
// is at URL. EnvNames and HeaderNames are the names of its secrets, which live in the OS secret
// store.
type MCPServer struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	Command     string   `json:"command,omitempty"`
	Args        []string `json:"args,omitempty"`
	URL         string   `json:"url,omitempty"`
	EnvNames    []string `json:"env_names,omitempty"`
	HeaderNames []string `json:"header_names,omitempty"`
}

const _mcpColumns = "id, name, kind, command, args, url, env_names, header_names"

func scanMCPServer(r *sql.Rows) (MCPServer, error) {
	var s MCPServer
	var args, env, headers string
	if err := r.Scan(&s.ID, &s.Name, &s.Kind, &s.Command, &args, &s.URL, &env, &headers); err != nil {
		return MCPServer{}, err
	}
	for _, f := range []struct {
		text string
		list *[]string
	}{{args, &s.Args}, {env, &s.EnvNames}, {headers, &s.HeaderNames}} {
		if err := json.Unmarshal([]byte(f.text), f.list); err != nil {
			return MCPServer{}, fmt.Errorf("decode MCP server %s: %w", s.ID, err)
		}
	}
	return s, nil
}

// mcpLists encodes the args, env names and header names of s as JSON arrays.
func mcpLists(s MCPServer) (lists [3]string, err error) {
	for i, list := range [][]string{s.Args, s.EnvNames, s.HeaderNames} {
		if list == nil {
			list = []string{}
		}
		b, err := json.Marshal(list)
		if err != nil {
			return lists, fmt.Errorf("encode MCP server: %w", err)
		}
		lists[i] = string(b)
	}
	return lists, nil
}

// MCPServers lists the user's MCP servers in the order added.
func (s *Store) MCPServers(ctx context.Context) ([]MCPServer, error) {
	return query(ctx, s.db, "SELECT "+_mcpColumns+" FROM mcp_servers ORDER BY rowid", scanMCPServer)
}

// MCPServer returns the user's MCP server with id.
func (s *Store) MCPServer(ctx context.Context, id string) (MCPServer, error) {
	rows, err := query(ctx, s.db,
		"SELECT "+_mcpColumns+" FROM mcp_servers WHERE id = ?", scanMCPServer, id)
	if err != nil {
		return MCPServer{}, fmt.Errorf("read MCP server %s: %w", id, err)
	}
	if len(rows) == 0 {
		return MCPServer{}, fmt.Errorf("MCP server %s: %w", id, ErrNotFound)
	}
	return rows[0], nil
}

// CreateMCPServer stores m with a new id. It wraps ErrExists when the name is taken.
func (s *Store) CreateMCPServer(ctx context.Context, m MCPServer) (MCPServer, error) {
	m.ID = newID()
	lists, err := mcpLists(m)
	if err != nil {
		return MCPServer{}, err
	}
	_, err = s.db.ExecContext(ctx,
		"INSERT INTO mcp_servers ("+_mcpColumns+") VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		m.ID, m.Name, m.Kind, m.Command, lists[0], m.URL, lists[1], lists[2])
	return m, mcpWriteError(err, m.Name)
}

// UpdateMCPServer replaces the server with m.ID. It wraps ErrExists when the name is taken.
func (s *Store) UpdateMCPServer(ctx context.Context, m MCPServer) (MCPServer, error) {
	lists, err := mcpLists(m)
	if err != nil {
		return MCPServer{}, err
	}
	res, err := s.db.ExecContext(ctx,
		"UPDATE mcp_servers SET name = ?, kind = ?, command = ?, args = ?, url = ?, env_names = ?, "+
			"header_names = ? WHERE id = ?",
		m.Name, m.Kind, m.Command, lists[0], m.URL, lists[1], lists[2], m.ID)
	if err = mcpWriteError(err, m.Name); err != nil {
		return MCPServer{}, err
	}
	return m, found(res, "MCP server", m.ID)
}

// DeleteMCPServer removes the server and drops it from every Bot's picks.
func (s *Store) DeleteMCPServer(ctx context.Context, id string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, "DELETE FROM mcp_servers WHERE id = ?", id)
		if err != nil {
			return fmt.Errorf("delete MCP server: %w", err)
		}
		if err := found(res, "MCP server", id); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx,
			"UPDATE bots SET mcp_servers = (SELECT json_group_array(value) FROM json_each(mcp_servers) "+
				"WHERE value != ?1) WHERE EXISTS (SELECT 1 FROM json_each(mcp_servers) WHERE value = ?1)", id)
		if err != nil {
			return fmt.Errorf("drop MCP server from bots: %w", err)
		}
		return nil
	})
}

func mcpWriteError(err error, name string) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "UNIQUE constraint failed: mcp_servers.name") {
		return fmt.Errorf("MCP server name %q: %w", name, ErrExists)
	}
	return fmt.Errorf("write MCP server: %w", err)
}
