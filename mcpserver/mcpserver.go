// Package mcpserver is the library of MCP servers Bots pick from: the catalog and the user's own,
// local or remote. Env and header values live in the OS secret store; the database keeps names.
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"

	"atoll/store"

	"github.com/zalando/go-keyring"
)

var ErrKeychain = errors.New("system keychain failed")

// ErrInvalid is wrapped when a server cannot be saved as given.
var ErrInvalid = errors.New("invalid MCP server")

var _name = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

var _catalog = []store.MCPServer{
	{ID: "context7", Name: "Context7", Kind: store.MCPCatalog, URL: "https://mcp.context7.com/mcp"},
	{
		ID: "notion", Name: "Notion", Kind: store.MCPCatalog, URL: "https://mcp.notion.com/mcp",
		Status: store.MCPNotConnected,
	},
	{
		ID: "figma", Name: "Figma", Kind: store.MCPCatalog, URL: "https://mcp.figma.com/mcp",
		Status: store.MCPNotConnected,
	},
	{
		ID: "cloudflare", Name: "Cloudflare", Kind: store.MCPCatalog,
		URL: "https://mcp.cloudflare.com/mcp", Status: store.MCPNotConnected,
	},
	{
		ID: SlackID, Name: "Slack", Kind: store.MCPCatalog, URL: "https://mcp.slack.com/mcp",
		Status: store.MCPNeedsSetup,
	},
}

// Input is a server as the user gives it: local with Command, remote with URL. A nil Env or
// Headers keeps every stored secret; otherwise it is the whole set, and a "" value keeps the
// stored value of that name.
type Input struct {
	Name    string            `json:"name"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

// List returns the catalog, then the user's servers in the order added.
func List(ctx context.Context, st *store.Store) ([]store.MCPServer, error) {
	own, err := st.MCPServers(ctx)
	if err != nil {
		return nil, err
	}
	signIns, err := st.MCPSignIns(ctx)
	if err != nil {
		return nil, err
	}
	servers := append(slices.Clone(_catalog), own...)
	for i, s := range servers {
		if status, ok := signIns[s.ID]; ok {
			servers[i].Status = status
		}
		if u := os.Getenv("ATOLL_SLACK_MCP_URL"); s.ID == SlackID && u != "" {
			servers[i].URL = u
		}
	}
	return servers, nil
}

// Get returns the server with id from the catalog or the user's servers.
func Get(ctx context.Context, st *store.Store, id string) (store.MCPServer, error) {
	servers, err := List(ctx, st)
	if err != nil {
		return store.MCPServer{}, err
	}
	if i := slices.IndexFunc(servers, func(s store.MCPServer) bool { return s.ID == id }); i >= 0 {
		return servers[i], nil
	}
	return store.MCPServer{}, fmt.Errorf("MCP server %s: %w", id, store.ErrNotFound)
}

// Add stores a new server and its secrets. A remote server given with no headers is asked once
// whether it needs sign-in.
func Add(ctx context.Context, st *store.Store, c *http.Client, in Input) (store.MCPServer, error) {
	s, write, err := in.plan(store.MCPServer{})
	if err != nil {
		return store.MCPServer{}, err
	}
	if s.Kind == store.MCPRemote && len(in.Headers) == 0 {
		s.Status = probe(ctx, c, s.URL)
	}
	if s, err = st.CreateMCPServer(ctx, s); err != nil {
		return store.MCPServer{}, err
	}
	if err := saveSecrets(s, write); err != nil {
		_ = st.DeleteMCPServer(ctx, s.ID)
		_ = deleteSecrets(s, nil)
		return store.MCPServer{}, err
	}
	if s.Status != "" {
		if err := st.SetMCPSignIn(ctx, s.ID, s.Status); err != nil {
			_ = st.DeleteMCPServer(ctx, s.ID)
			return store.MCPServer{}, err
		}
	}
	return s, nil
}

// Replace drops the sign-in when the URL or kind changes, and probes a remote server left with no
// headers.
func Replace(ctx context.Context, st *store.Store, c *http.Client, id string, in Input) (store.MCPServer, error) {
	lock, _ := _tokenLocks.LoadOrStore(id, &sync.Mutex{})
	mu, _ := lock.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	prev, err := st.MCPServer(ctx, id)
	if err != nil {
		return store.MCPServer{}, err
	}
	s, write, err := in.plan(prev)
	if err != nil {
		return store.MCPServer{}, err
	}
	if err := saveSecrets(s, write); err != nil {
		return store.MCPServer{}, err
	}
	changed := prev.URL != s.URL || prev.Kind != s.Kind
	if changed {
		if err := errors.Join(deleteTokens(id), st.DeleteMCPSignIn(ctx, id)); err != nil {
			return store.MCPServer{}, err
		}
	}
	if s, err = st.UpdateMCPServer(ctx, s); err != nil {
		return store.MCPServer{}, err
	}
	if changed {
		if s.Kind == store.MCPRemote && len(s.HeaderNames) == 0 {
			if s.Status = probe(ctx, c, s.URL); s.Status != "" {
				if err := st.SetMCPSignIn(ctx, id, s.Status); err != nil {
					return store.MCPServer{}, err
				}
			}
		}
	}
	kept := secretNames(s)
	if prev.Kind != s.Kind {
		kept = nil
	}
	return s, deleteSecrets(prev, kept)
}

// Remove on a catalog server with a sign-in deletes its tokens and resets its status.
func Remove(ctx context.Context, st *store.Store, id string) error {
	if i := slices.IndexFunc(_catalog, func(c store.MCPServer) bool { return c.ID == id }); i >= 0 {
		if _catalog[i].Status == "" {
			return fmt.Errorf("MCP server %s: %w", id, store.ErrNotFound)
		}
		return errors.Join(deleteTokens(id), st.SetMCPSignIn(ctx, id, store.MCPNotConnected))
	}
	s, err := st.MCPServer(ctx, id)
	if err != nil {
		return err
	}
	if err := st.DeleteMCPServer(ctx, id); err != nil {
		return err
	}
	return errors.Join(deleteSecrets(s, nil), deleteTokens(id))
}

// Secrets returns the stored env values of a local server, or the header values of a remote one.
func Secrets(s store.MCPServer) (map[string]string, error) {
	values := map[string]string{}
	for _, name := range secretNames(s) {
		v, err := keyring.Get("atoll", secretUser(s, name))
		if err != nil {
			return nil, fmt.Errorf("read %s of MCP server %s: %w", name, s.Name, err)
		}
		values[name] = v
	}
	return values, nil
}

func (in Input) plan(prev store.MCPServer) (s store.MCPServer, write map[string]string, err error) {
	s = store.MCPServer{ID: prev.ID, Name: in.Name, Command: in.Command, Args: in.Args, URL: in.URL}
	given := in.Headers
	switch {
	case !_name.MatchString(in.Name) || strings.Contains(in.Name, "__"):
		return s, nil, invalid("name must be letters, digits, - or _, and not contain __")
	case slices.ContainsFunc(_catalog, func(c store.MCPServer) bool { return strings.EqualFold(in.Name, c.Name) }):
		return s, nil, fmt.Errorf("MCP server name %q: %w", in.Name, store.ErrExists)
	case (in.Command == "") == (in.URL == ""):
		return s, nil, invalid("give a command or a URL, not both or neither")
	case in.Command != "":
		s.Kind, given = store.MCPLocal, in.Env
		if len(in.Headers) > 0 {
			return s, nil, invalid("a local server has no headers")
		}
	default:
		s.Kind = store.MCPRemote
		if u, err := url.Parse(in.URL); err != nil || u.Host == "" || u.Scheme != "http" && u.Scheme != "https" {
			return s, nil, invalid("url must be http or https")
		}
		if len(in.Args) > 0 || len(in.Env) > 0 {
			return s, nil, invalid("a remote server has no args or env")
		}
	}
	names := secretNames(prev)
	if prev.Kind != s.Kind {
		names = nil
	}
	if given == nil {
		setNames(&s, names)
		return s, nil, nil
	}
	write = map[string]string{}
	var next []string
	for name, value := range given {
		switch {
		case name == "":
			return s, nil, invalid("a secret needs a name")
		case value != "":
			write[name] = value
		case !slices.Contains(names, name):
			return s, nil, invalid(name + " has no stored value")
		}
		next = append(next, name)
	}
	slices.Sort(next)
	setNames(&s, next)
	return s, write, nil
}

func invalid(msg string) error { return fmt.Errorf("%w: %s", ErrInvalid, msg) }

func secretNames(s store.MCPServer) []string {
	if s.Kind == store.MCPLocal {
		return s.EnvNames
	}
	return s.HeaderNames
}

func setNames(s *store.MCPServer, names []string) {
	if s.Kind == store.MCPLocal {
		s.EnvNames = names
	} else {
		s.HeaderNames = names
	}
}

func secretUser(s store.MCPServer, name string) string {
	kind := "header"
	if s.Kind == store.MCPLocal {
		kind = "env"
	}
	return "mcp:" + s.ID + ":" + kind + ":" + name
}

func saveSecrets(s store.MCPServer, values map[string]string) error {
	for name, value := range values {
		if err := keyring.Set("atoll", secretUser(s, name), value); err != nil {
			return fmt.Errorf("save %s of MCP server %s: %w: %w", name, s.Name, ErrKeychain, err)
		}
	}
	return nil
}

func deleteSecrets(s store.MCPServer, keep []string) error {
	var errs []error
	for _, name := range secretNames(s) {
		if slices.Contains(keep, name) {
			continue
		}
		if err := keyring.Delete("atoll", secretUser(s, name)); err != nil && !errors.Is(err, keyring.ErrNotFound) {
			errs = append(errs, fmt.Errorf("delete %s of MCP server %s: %w: %w", name, s.Name, ErrKeychain, err))
		}
	}
	return errors.Join(errs...)
}
