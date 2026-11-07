package server

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"atoll/provider"
	"atoll/repo"
	"atoll/run"
	"atoll/skill"
	"atoll/store"
)

var _looks = []string{"jellyfish", "whale", "penguin", "shark", "starfish", "anglerfish", "sand_dollar", "sponge", "kelp", "shell", "wave", "island", "sun", "sailboat"}

var _modes = []string{provider.ModeAuto, provider.ModeAsk, provider.ModeFull}

func (a *api) createWithLook(ctx context.Context, b store.Bot) (store.Bot, error) {
	if b.Look == "" {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(_looks))))
		if err != nil {
			return store.Bot{}, err
		}
		b.Look = _looks[n.Int64()]
	}
	return a.st.CreateBot(ctx, b)
}

// botProblem says why b cannot be saved, or "" when it can.
func botProblem(b store.Bot) string {
	switch {
	case b.Name == "" || (b.Provider != "claude" && b.Provider != "codex"):
		return "name is required and provider must be claude or codex"
	case b.Look != "" && !slices.Contains(_looks, b.Look):
		return fmt.Sprintf("unknown look %q; looks: %s", b.Look, strings.Join(_looks, ", "))
	case b.Mode != "" && !slices.Contains(_modes, b.Mode):
		return fmt.Sprintf("unknown mode %q; modes: %s", b.Mode, strings.Join(_modes, ", "))
	}
	return ""
}

type botResponse struct {
	store.Bot
	Look         string     `json:"look"`
	Model        string     `json:"model"`
	Pinned       bool       `json:"pinned"`
	Activity     string     `json:"activity"`
	LastActivity *time.Time `json:"last_activity"`
}

func responseBot(b store.Bot) botResponse {
	return botResponse{Bot: b, Look: b.Look, Model: b.Model, Pinned: b.Pinned, Activity: b.Activity, LastActivity: b.LastActivity}
}

func (a *api) botSummary(
	ctx context.Context, b store.Bot, threads, helpers []store.ThreadRun,
) store.Bot {
	latest := ""
	for _, t := range threads {
		if latest == "" {
			latest = t.Status
			at := t.LastActivity
			b.LastActivity = &at
			switch latest {
			case "running", "waiting":
				b.Activity = a.liveVerb(t.ThreadID)
			case "done":
				b.Activity = preview(t.Reply)
			case "failed":
				b.Activity = "Run failed"
			case "stopped":
				b.Activity = "Stopped"
			}
		}
		if t.Status != "running" && t.Status != "waiting" {
			continue
		}
		if _, approvals, _ := a.runs.Active(t.ThreadID); len(approvals) > 0 {
			b.State = "needs_you"
			b.Activity = needsYou(approvals[0])
			return b
		}
		b.State = "working"
	}
	for _, h := range helpers {
		if _, approvals, _ := a.runs.Active(h.ThreadID); len(approvals) > 0 {
			b.State = "needs_you"
			b.Activity = needsYou(approvals[0])
			return b
		}
	}
	if b.State != "" {
		return b
	}
	if latest == "failed" {
		b.State = "failed"
	} else if !a.runs.Available(ctx, b.Provider) {
		b.State = "asleep"
	} else {
		b.State = "idle"
	}
	return b
}

func needsYou(ap run.Approval) string {
	kind, target := run.ToolKind(ap.Tool, ap.Input)
	verb := "use"
	switch kind {
	case "search", "read", "edit":
		verb = kind
	case "command":
		verb = "run"
	}
	if tool := ap.Tool; tool != "" && unicode.IsUpper([]rune(tool)[0]) {
		verb = tool
	}
	return strings.TrimSpace("Needs you: " + verb + " " + target)
}

const _previewLen = 60

// preview is the first line of a reply, at most _previewLen characters cut at a word boundary,
// with an ellipsis when cut.
func preview(reply string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(reply), "\n")
	runes := []rune(strings.TrimSpace(line))
	if len(runes) <= _previewLen {
		return string(runes)
	}
	cut := runes[:_previewLen]
	if runes[_previewLen] != ' ' {
		for i := len(cut) - 1; i > 0; i-- {
			if cut[i] == ' ' {
				cut = cut[:i]
				break
			}
		}
	}
	return strings.TrimSpace(string(cut)) + "…"
}

func (a *api) getBot(w http.ResponseWriter, r *http.Request) {
	b, err := a.st.Bot(r.Context(), r.PathValue("id"))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	threads, helpers, err := a.threadRuns(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusOK, responseBot(a.botSummary(r.Context(), b, threads[b.ID], helpers[b.ID])))
}

func (a *api) pinBot(w http.ResponseWriter, r *http.Request) {
	a.setPinned(w, r, true)
}

func (a *api) unpinBot(w http.ResponseWriter, r *http.Request) {
	a.setPinned(w, r, false)
}

func (a *api) setPinned(w http.ResponseWriter, r *http.Request, pinned bool) {
	id := r.PathValue("id")
	if id == "chief" {
		fail(w, http.StatusConflict, "Chief is always first")
		return
	}
	if err := a.st.SetPinned(r.Context(), id, pinned); err != nil {
		a.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

const _chiefRepoRefusal = "Chief has no repo access and cannot be given any"

func chiefHasRepos(b store.Bot) bool {
	return b.ID == _chiefID && (b.RepoAccess == nil || len(b.RepoAccess) > 0)
}

func (a *api) putBot(w http.ResponseWriter, r *http.Request) {
	b, err := a.st.Bot(r.Context(), r.PathValue("id"))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if json.NewDecoder(r.Body).Decode(&b) != nil {
		fail(w, http.StatusBadRequest, "invalid bot")
		return
	}
	if msg := botProblem(b); msg != "" {
		fail(w, http.StatusBadRequest, msg)
		return
	}
	b.ID = r.PathValue("id")
	msg, err := a.unknownPicks(r.Context(), b)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if msg != "" {
		fail(w, http.StatusBadRequest, msg)
		return
	}
	if chiefHasRepos(b) {
		fail(w, http.StatusBadRequest, _chiefRepoRefusal)
		return
	}
	b, err = a.st.UpdateBot(r.Context(), b)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusOK, responseBot(b))
}

func (a *api) deleteBot(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("id") == _chiefID {
		fail(w, http.StatusConflict, "Chief cannot be deleted")
		return
	}
	if err := a.runs.DeleteBot(r.Context(), r.PathValue("id")); err != nil {
		a.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) createSkill(w http.ResponseWriter, r *http.Request) {
	var sk store.Skill
	if err := readSkill(r, &sk); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	sk, err := a.st.CreateSkill(r.Context(), sk)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusCreated, sk)
}

func (a *api) listSkills(w http.ResponseWriter, r *http.Request) {
	sk, err := a.st.Skills(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusOK, orEmpty(sk))
}

func (a *api) getSkill(w http.ResponseWriter, r *http.Request) {
	sk, err := a.st.Skill(r.Context(), r.PathValue("id"))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusOK, sk)
}

func (a *api) putSkill(w http.ResponseWriter, r *http.Request) {
	sk, err := a.st.Skill(r.Context(), r.PathValue("id"))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if err := readSkill(r, &sk); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	sk, err = a.st.UpdateSkill(r.Context(), sk)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusOK, sk)
}

// reimportSkill reads a skill's source folder again, as import_skill does.
func (a *api) reimportSkill(w http.ResponseWriter, r *http.Request) {
	sk, err := a.st.Skill(r.Context(), r.PathValue("id"))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if sk.Source == "" {
		fail(w, http.StatusConflict, "skill was not imported from a folder")
		return
	}
	var in struct {
		Replace bool `json:"replace"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		fail(w, http.StatusBadRequest, "invalid body")
		return
	}
	res, _, _, err := a.importSkill(r.Context(), sk.Source, in.Replace)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	write(w, http.StatusOK, res)
}

func (a *api) undoChange(w http.ResponseWriter, r *http.Request) {
	_, err := a.st.UndoChange(r.Context(), r.PathValue("id"))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) listChanges(w http.ResponseWriter, r *http.Request) {
	changes, err := a.st.Changes(r.Context(), r.PathValue("id"))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusOK, orEmpty(changes))
}

const _platformTools = `[
{"name":"create_bot","description":"Create a Bot.","inputSchema":{"type":"object","properties":{"name":{"type":"string"},"persona":{"type":"string"},"provider":{"type":"string","enum":["claude","codex"]},"skills":{"type":"array","items":{"type":"string"}},"repo_access":{"type":["array","null"],"items":{"type":"string"}},"connections":{"type":"array","items":{"type":"string"}},"mcp_servers":{"type":"array","items":{"type":"string"}},"model":{"type":"string"},"look":{"type":"string"},"mode":{"type":"string","enum":["auto","ask","full"]},"delegates":{"type":"array","items":{"type":"string"}}},"required":["name","provider"]}},
{"name":"edit_bot","description":"Edit a Bot.","inputSchema":{"type":"object","properties":{"id":{"type":"string"},"name":{"type":"string"},"persona":{"type":"string"},"provider":{"type":"string","enum":["claude","codex"]},"skills":{"type":"array","items":{"type":"string"}},"repo_access":{"type":["array","null"],"items":{"type":"string"}},"connections":{"type":"array","items":{"type":"string"}},"mcp_servers":{"type":"array","items":{"type":"string"}},"model":{"type":"string"},"look":{"type":"string"},"mode":{"type":"string","enum":["auto","ask","full"]},"delegates":{"type":"array","items":{"type":"string"}}},"required":["id"]}},
{"name":"create_skill","description":"Create a skill.","inputSchema":{"type":"object","properties":{"name":{"type":"string"},"content":{"type":"string"}},"required":["name","content"]}},
{"name":"edit_skill","description":"Edit a skill.","inputSchema":{"type":"object","properties":{"id":{"type":"string"},"name":{"type":"string"},"content":{"type":"string"}},"required":["id"]}},
{"name":"import_skill","description":"Import a local skill folder (SKILL.md and its text files), or update the skill imported from it. Returns what it added, changed, removed and skipped.","inputSchema":{"type":"object","properties":{"path":{"type":"string"},"replace":{"type":"boolean","description":"Overwrite edits made in Reef. Only after the user agreed."}},"required":["path"]}},
{"name":"register_repo","description":"Register a local Git repository.","inputSchema":{"type":"object","properties":{"name":{"type":"string"},"path":{"type":"string"}},"required":["name","path"]}},
{"name":"attach_connection","description":"Attach a connected account to a Bot.","inputSchema":{"type":"object","properties":{"bot_id":{"type":"string"},"connection_id":{"type":"string"}},"required":["bot_id","connection_id"]}},
{"name":"list_bots","description":"List every Bot.","inputSchema":{"type":"object","properties":{}}},
{"name":"list_skills","description":"List the skills in the library, without their content.","inputSchema":{"type":"object","properties":{}}},
{"name":"list_mcp_servers","description":"List the MCP servers in the library: the catalog and the user's own, without their secrets.","inputSchema":{"type":"object","properties":{}}},
{"name":"list_connections","description":"List the connected accounts.","inputSchema":{"type":"object","properties":{}}},
{"name":"list_repos","description":"List the registered repos.","inputSchema":{"type":"object","properties":{}}},
{"name":"list_models","description":"List the signed-in providers and their models.","inputSchema":{"type":"object","properties":{}}}
]`

// Providers serves /api/providers and tells Chief's tools the state it caches for it.
type Providers interface {
	http.Handler
	Cached(ctx context.Context) []Provider
}

// Provider is what GET /api/providers lists for a provider that Chief's tools need.
type Provider struct {
	Name     string  `json:"name"`
	Label    string  `json:"label"`
	SignedIn bool    `json:"signed_in"`
	Models   []Model `json:"models"`
}

// Model is one entry of a provider's model list.
type Model struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

var _readTools = []string{
	provider.ToolListBots, provider.ToolListSkills, provider.ToolListConnections,
	provider.ToolListRepos, provider.ToolListModels, provider.ToolListMCPServers,
}

func isPlatformTool(name string) bool {
	return slices.Contains(_readTools, name) || slices.Contains([]string{
		provider.ToolCreateBot, provider.ToolEditBot, provider.ToolCreateSkill,
		provider.ToolEditSkill, provider.ToolImportSkill, provider.ToolRegisterRepo,
		provider.ToolAttachConnection,
	}, name)
}

func (a *api) readCall(ctx context.Context, name string) (any, *rpcError) {
	var v any
	var err error
	switch name {
	case provider.ToolListBots:
		v, err = a.botList(ctx)
	case provider.ToolListSkills:
		var skills []store.Skill
		skills, err = a.st.Skills(ctx)
		items := make([]struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}, len(skills))
		for i, sk := range skills {
			items[i].ID, items[i].Name = sk.ID, sk.Name
		}
		v = items
	case provider.ToolListConnections:
		var conns []store.Connection
		conns, err = a.st.Connections(ctx)
		v = orEmpty(conns)
	case provider.ToolListRepos:
		var repos []store.Repo
		repos, err = a.st.Repos(ctx)
		v = orEmpty(repos)
	case provider.ToolListModels:
		v = a.signedInProviders(ctx)
	case provider.ToolListMCPServers:
		v, err = a.mcpLibrary(ctx)
	}
	if err != nil {
		return nil, a.toolError(err)
	}
	return a.jsonText(v)
}

// signedInProviders is what GET /api/providers answers for the signed-in providers, so Chief sees
// the New Bot model picker.
func (a *api) signedInProviders(ctx context.Context) []Provider {
	signedIn := []Provider{}
	for _, p := range a.cachedProviders(ctx) {
		if p.SignedIn {
			signedIn = append(signedIn, p)
		}
	}
	return signedIn
}

func (a *api) cachedProviders(ctx context.Context) []Provider {
	if a.providers == nil {
		return nil
	}
	return a.providers.Cached(ctx)
}

func (a *api) platformCall(
	ctx context.Context, token, name string, args json.RawMessage,
) (any, *rpcError) {
	source, thread, runID, err := a.runs.Source(token)
	if err != nil {
		return nil, a.toolError(err)
	}
	if !source.CanManagePlatform {
		return nil, &rpcError{Code: _invalidParams, Message: "platform access denied"}
	}
	if slices.Contains(_readTools, name) {
		return a.readCall(ctx, name)
	}
	var before, after, reply any
	var entity, id, action string
	action = store.ActionEdit
	switch name {
	case provider.ToolCreateBot:
		var b store.Bot
		if json.Unmarshal(args, &b) != nil {
			return nil, &rpcError{Code: _invalidParams, Message: "invalid bot"}
		}
		if msg := botProblem(b); msg != "" {
			return nil, &rpcError{Code: _invalidParams, Message: msg}
		}
		if rerr := a.botChoiceError(ctx, b, store.Bot{}); rerr != nil {
			return nil, rerr
		}
		b.CanManagePlatform = false
		b, err = a.createWithLook(ctx, b)
		after, entity, id, action = b, store.EntityBot, b.ID, store.ActionCreate
	case provider.ToolEditBot:
		var in struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(args, &in) != nil || in.ID == "" {
			return nil, &rpcError{Code: _invalidParams, Message: "invalid bot id"}
		}
		var prev, b store.Bot
		prev, err = a.st.Bot(ctx, in.ID)
		if err == nil {
			before = prev
			b = prev
			b.Skills = slices.Clone(prev.Skills)
			b.Connections = slices.Clone(prev.Connections)
			b.MCPServers = slices.Clone(prev.MCPServers)
			b.Delegates = slices.Clone(prev.Delegates)
			b.RepoAccess = slices.Clone(prev.RepoAccess)
			if json.Unmarshal(args, &b) != nil {
				return nil, &rpcError{Code: _invalidParams, Message: "invalid bot"}
			}
			b.ID, b.CanManagePlatform = in.ID, prev.CanManagePlatform
			if chiefHasRepos(b) {
				return nil, &rpcError{Code: _invalidParams, Message: _chiefRepoRefusal}
			}
			if msg := botProblem(b); msg != "" {
				return nil, &rpcError{Code: _invalidParams, Message: msg}
			}
			if rerr := a.botChoiceError(ctx, b, prev); rerr != nil {
				return nil, rerr
			}
			b, err = a.st.UpdateBot(ctx, b)
		}
		after, entity, id = b, store.EntityBot, in.ID
	case provider.ToolCreateSkill:
		var sk store.Skill
		if derr := decodeSkill(args, &sk); derr != nil {
			return nil, &rpcError{Code: _invalidParams, Message: derr.Error()}
		}
		sk, err = a.st.CreateSkill(ctx, sk)
		after, entity, id, action = sk, store.EntitySkill, sk.ID, store.ActionCreate
	case provider.ToolEditSkill:
		var in struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(args, &in) != nil || in.ID == "" {
			return nil, &rpcError{Code: _invalidParams, Message: "invalid skill id"}
		}
		var sk store.Skill
		sk, err = a.st.Skill(ctx, in.ID)
		if err == nil {
			before = sk
			if derr := decodeSkill(args, &sk); derr != nil {
				return nil, &rpcError{Code: _invalidParams, Message: derr.Error()}
			}
			sk.ID = in.ID
			sk, err = a.st.UpdateSkill(ctx, sk)
		}
		after, entity, id = sk, store.EntitySkill, in.ID
	case provider.ToolImportSkill:
		var in struct {
			Path    string `json:"path"`
			Replace bool   `json:"replace"`
		}
		if json.Unmarshal(args, &in) != nil || in.Path == "" {
			return nil, &rpcError{Code: _invalidParams, Message: "invalid path"}
		}
		var res importResult
		var prev, sk store.Skill
		res, prev, sk, err = a.importSkill(ctx, in.Path, in.Replace)
		if err == nil && (res.Status == _importUnchanged || res.Status == _importConfirm) {
			return a.jsonText(res)
		}
		reply, after, entity, id = res, sk, store.EntitySkill, sk.ID
		if res.Status == _importCreated {
			action = store.ActionCreate
		} else {
			before = prev
		}
	case provider.ToolRegisterRepo:
		var rp store.Repo
		if json.Unmarshal(args, &rp) != nil {
			return nil, &rpcError{Code: _invalidParams, Message: "invalid repo"}
		}
		rp, err = a.registerRepo(ctx, rp)
		after, entity, id, action = rp, store.EntityRepo, rp.ID, store.ActionCreate
	case provider.ToolAttachConnection:
		var in struct {
			BotID        string `json:"bot_id"`
			ConnectionID string `json:"connection_id"`
		}
		if json.Unmarshal(args, &in) != nil || in.BotID == "" || in.ConnectionID == "" {
			return nil, &rpcError{Code: _invalidParams, Message: "invalid connection"}
		}
		var b store.Bot
		b, err = a.st.Bot(ctx, in.BotID)
		if err == nil {
			var c store.Connection
			c, err = a.st.Connection(ctx, in.ConnectionID)
			if err == nil && c.Status != store.ConnectionConnected {
				err = errors.New("connection is not connected")
			}
			if err == nil {
				before = b
				b.Connections = slices.Clone(b.Connections)
				if !slices.Contains(b.Connections, in.ConnectionID) {
					b.Connections = append(b.Connections, in.ConnectionID)
				}
				b, err = a.st.UpdateBot(ctx, b)
			}
		}
		after, entity, id = b, store.EntityBot, in.BotID
	}
	if err != nil {
		return nil, a.toolError(err)
	}
	oldJSON, err := json.Marshal(before)
	if err != nil {
		return nil, a.toolError(err)
	}
	newJSON, err := json.Marshal(after)
	if err != nil {
		return nil, a.toolError(err)
	}
	change, err := a.st.RecordChange(ctx, store.Change{
		Entity: entity, EntityID: id, Action: action, Before: oldJSON, After: newJSON,
		SourceBotID: source.ID, SourceThreadID: thread.ID, SourceRunID: runID,
	})
	if err != nil {
		return nil, a.toolError(err)
	}
	a.runs.Changed(token, change)
	if reply == nil {
		reply = after
	}
	return a.jsonText(reply)
}

// botChoiceError rejects a repo id no registered repo has, a model the Bot's provider does not list,
// and a provider that is not signed in; the model check is skipped when the Bot keeps the provider and
// model it has.
func (a *api) botChoiceError(ctx context.Context, b, current store.Bot) *rpcError {
	msg, err := a.unknownPicks(ctx, b)
	if err != nil {
		return a.toolError(err)
	}
	if msg != "" {
		return &rpcError{Code: _invalidParams, Message: msg}
	}
	if b.Model == "" || (b.Model == current.Model && b.Provider == current.Provider) {
		return nil
	}
	var models []string
	for _, p := range a.cachedProviders(ctx) {
		if p.Name != b.Provider {
			continue
		}
		if !p.SignedIn {
			return &rpcError{Code: _invalidParams, Message: p.Name + " is not signed in"}
		}
		for _, m := range p.Models {
			models = append(models, m.ID)
		}
	}
	if !slices.Contains(models, b.Model) {
		return &rpcError{Code: _invalidParams, Message: fmt.Sprintf(
			"unknown model %q; models: %s", b.Model, strings.Join(models, ", "))}
	}
	return nil
}

func (a *api) unknownPicks(ctx context.Context, b store.Bot) (string, error) {
	msg, err := a.unknownRepos(ctx, b.RepoAccess)
	if err != nil || msg != "" {
		return msg, err
	}
	msg, err = a.unknownMCPServers(ctx, b.MCPServers)
	if err != nil || msg != "" {
		return msg, err
	}
	return a.unknownDelegates(ctx, b)
}

func (a *api) unknownDelegates(ctx context.Context, b store.Bot) (string, error) {
	if len(b.Delegates) == 0 {
		return "", nil
	}
	bots, err := a.st.Bots(ctx)
	if err != nil {
		return "", err
	}
	var ids, others []string
	for _, o := range bots {
		if o.ID != b.ID {
			ids = append(ids, o.ID)
			others = append(others, fmt.Sprintf("%s (%s)", o.Name, o.ID))
		}
	}
	var unknown []string
	for _, id := range b.Delegates {
		if !slices.Contains(ids, id) {
			unknown = append(unknown, strconv.Quote(id))
		}
	}
	if len(unknown) == 0 {
		return "", nil
	}
	return fmt.Sprintf("unknown delegate ids %s; Bots: %s",
		strings.Join(unknown, ", "), strings.Join(others, ", ")), nil
}

// unknownRepos is the message for repo ids no registered repo has, or "" when there are none.
func (a *api) unknownRepos(ctx context.Context, access store.RepoAccess) (string, error) {
	if len(access) == 0 {
		return "", nil
	}
	repos, err := a.st.Repos(ctx)
	if err != nil {
		return "", err
	}
	ids := make([]string, len(repos))
	for i, rp := range repos {
		ids[i] = rp.ID
	}
	var unknown []string
	for _, id := range access {
		if !slices.Contains(ids, id) {
			unknown = append(unknown, strconv.Quote(id))
		}
	}
	if len(unknown) == 0 {
		return "", nil
	}
	if len(ids) == 0 {
		return fmt.Sprintf("unknown repo ids %s; no repos are registered", strings.Join(unknown, ", ")), nil
	}
	return fmt.Sprintf("unknown repo ids %s; registered repos: %s",
		strings.Join(unknown, ", "), strings.Join(ids, ", ")), nil
}

func (a *api) registerRepo(ctx context.Context, rp store.Repo) (store.Repo, error) {
	if err := validateRepo(ctx, rp); err != nil {
		return store.Repo{}, err
	}
	return a.st.CreateRepo(ctx, rp.Name, rp.Path)
}

func validateRepo(ctx context.Context, rp store.Repo) error {
	if rp.Name == "" {
		return errors.New("name and path are required")
	}
	if !filepath.IsAbs(rp.Path) || !repo.IsRepo(ctx, rp.Path) {
		return errors.New("path must be the top level of a git repo")
	}
	return nil
}

// Statuses of an import.
const (
	_importCreated   = "created"
	_importReplaced  = "replaced"
	_importUnchanged = "unchanged"
	_importConfirm   = "confirm"
)

// importResult is what import_skill answers: the paths it added, changed and removed against the
// skill it replaces, and what it left out of the folder.
type importResult struct {
	Status    string          `json:"status"`
	Skill     *importedSkill  `json:"skill,omitempty"`
	Added     []string        `json:"added"`
	Changed   []string        `json:"changed"`
	Removed   []string        `json:"removed"`
	Skipped   []skill.Skipped `json:"skipped"`
	HomePaths []string        `json:"home_paths"`
	Warning   string          `json:"warning,omitempty"`
}

type importedSkill struct {
	ID     string         `json:"id"`
	Name   string         `json:"name"`
	Source string         `json:"source"`
	Files  []importedFile `json:"files"`
}

type importedFile struct {
	Path       string `json:"path"`
	Executable bool   `json:"executable"`
}

// importSkill reads the local skill folder at path into the library. It updates the skill imported
// from the same folder, or a skill of the same name made in Reef, and asks for replace before it
// overwrites edits made in Reef. It returns the stored skill before and after.
func (a *api) importSkill(
	ctx context.Context, path string, replace bool,
) (res importResult, prev, next store.Skill, err error) {
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			return res, prev, next, fmt.Errorf("find home: %w", err)
		}
		path = filepath.Join(home, rest)
	}
	if !filepath.IsAbs(path) {
		return res, prev, next, errors.New("path must be absolute or start with ~/")
	}
	imp, err := skill.Read(filepath.Clean(path))
	if err != nil {
		return res, prev, next, err
	}
	next = imp.Skill
	next.Imported = skill.Hash(next)
	res = importResult{
		Status: _importCreated, Added: []string{}, Changed: []string{}, Removed: []string{},
		Skipped: orEmpty(imp.Skipped), HomePaths: orEmpty(imp.HomePaths),
	}
	skills, err := a.st.Skills(ctx)
	if err != nil {
		return res, prev, next, err
	}
	i := slices.IndexFunc(skills, func(sk store.Skill) bool { return sk.Source == next.Source })
	if i < 0 {
		i = slices.IndexFunc(skills, func(sk store.Skill) bool {
			return sk.Source == "" && sk.Name == next.Name
		})
	}
	if i < 0 {
		next, err = a.st.CreateSkill(ctx, next)
		res.Skill = summarize(next)
		return res, prev, next, err
	}
	prev = skills[i]
	next.ID = prev.ID
	res.Added, res.Changed, res.Removed = skill.Diff(prev, next)
	switch {
	case len(res.Added)+len(res.Changed)+len(res.Removed) == 0:
		res.Status, res.Skill = _importUnchanged, summarize(prev)
		return res, prev, prev, nil
	case skill.Hash(prev) != prev.Imported && !replace:
		res.Status = _importConfirm
		res.Warning = "The skill was edited in Reef since its last import; those Reef edits will be " +
			"overwritten. Ask the user, then call import_skill again with replace: true."
		return res, prev, prev, nil
	}
	next, err = a.st.UpdateSkill(ctx, next)
	res.Status, res.Skill = _importReplaced, summarize(next)
	return res, prev, next, err
}

func summarize(sk store.Skill) *importedSkill {
	out := &importedSkill{ID: sk.ID, Name: sk.Name, Source: sk.Source,
		Files: make([]importedFile, len(sk.Files))}
	for i, f := range sk.Files {
		out.Files[i] = importedFile{Path: f.Path, Executable: f.Executable}
	}
	return out
}

// readSkill decodes a skill request body over sk.
func readSkill(r *http.Request, sk *store.Skill) error {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	return decodeSkill(body, sk)
}

// decodeSkill decodes a skill over sk and checks it. Absent or null files keep sk's files. JSON
// decoding turns invalid UTF-8 into U+FFFD, so the raw body is checked first.
func decodeSkill(body []byte, sk *store.Skill) error {
	if !utf8.Valid(body) {
		return errors.New("skill is not UTF-8 text")
	}
	id, source, files := sk.ID, sk.Source, sk.Files
	sk.Files = nil
	if json.Unmarshal(body, sk) != nil || sk.Name == "" {
		return errors.New("invalid skill: name is required")
	}
	sk.ID, sk.Source = id, source
	if sk.Files == nil {
		sk.Files = files
	}
	return skill.Check(sk.Files)
}
