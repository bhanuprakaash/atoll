package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"atoll/provider"
	"atoll/repo"
	"atoll/run"
	"atoll/store"
)

var _looks = []string{"jellyfish", "whale", "penguin", "shark", "octopus", "turtle", "seahorse", "otter", "clownfish", "manta"}

func validBot(b store.Bot) bool {
	return b.Name != "" && (b.Provider == "claude" || b.Provider == "codex") &&
		(b.Look == "" || slices.Contains(_looks, b.Look))
}

type botResponse struct {
	store.Bot
	Look         string     `json:"look"`
	Model        string     `json:"model"`
	Activity     string     `json:"activity"`
	LastActivity *time.Time `json:"last_activity"`
}

func responseBot(b store.Bot) botResponse {
	return botResponse{Bot: b, Look: b.Look, Model: b.Model, Activity: b.Activity, LastActivity: b.LastActivity}
}

func (a *api) botSummary(r *http.Request, b store.Bot, threads []store.ThreadRun) store.Bot {
	latest := ""
	for _, t := range threads {
		if latest == "" {
			latest = t.Status
			at := t.LastActivity
			b.LastActivity = &at
			switch latest {
			case "running", "waiting":
				b.Activity = "Working"
			case "done":
				b.Activity = "Done"
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
			kind, target := run.ToolKind(approvals[0].Tool, approvals[0].Input)
			verb := "use"
			switch kind {
			case "search", "read", "edit":
				verb = kind
			case "command":
				verb = "run"
			}
			b.Activity = strings.TrimSpace("Needs you: " + verb + " " + target)
			return b
		}
		b.State = "working"
	}
	if b.State != "" {
		return b
	}
	if latest == "failed" {
		b.State = "failed"
	} else if !a.runs.Available(r.Context(), b.Provider) {
		b.State = "asleep"
	} else {
		b.State = "idle"
	}
	return b
}

func (a *api) getBot(w http.ResponseWriter, r *http.Request) {
	b, err := a.st.Bot(r.Context(), r.PathValue("id"))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	threads, err := a.st.ThreadRuns(r.Context(), b.ID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusOK, responseBot(a.botSummary(r, b, threads)))
}

func (a *api) putBot(w http.ResponseWriter, r *http.Request) {
	b, err := a.st.Bot(r.Context(), r.PathValue("id"))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if json.NewDecoder(r.Body).Decode(&b) != nil || !validBot(b) {
		fail(w, http.StatusBadRequest, "invalid bot")
		return
	}
	b.ID = r.PathValue("id")
	b, err = a.st.UpdateBot(r.Context(), b)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusOK, responseBot(b))
}

func (a *api) createSkill(w http.ResponseWriter, r *http.Request) {
	var sk store.Skill
	if json.NewDecoder(r.Body).Decode(&sk) != nil || sk.Name == "" {
		fail(w, http.StatusBadRequest, "name is required")
		return
	}
	sk, err := a.st.CreateSkill(r.Context(), sk.Name, sk.Content)
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
	if json.NewDecoder(r.Body).Decode(&sk) != nil || sk.Name == "" {
		fail(w, http.StatusBadRequest, "invalid skill")
		return
	}
	sk.ID = r.PathValue("id")
	sk, err = a.st.UpdateSkill(r.Context(), sk)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusOK, sk)
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
{"name":"create_bot","description":"Create a Bot.","inputSchema":{"type":"object","properties":{"name":{"type":"string"},"persona":{"type":"string"},"provider":{"type":"string","enum":["claude","codex"]},"skills":{"type":"array","items":{"type":"string"}},"repo_access":{"type":["array","null"],"items":{"type":"string"}},"connections":{"type":"array","items":{"type":"string"}}},"required":["name","provider"]}},
{"name":"edit_bot","description":"Edit a Bot.","inputSchema":{"type":"object","properties":{"id":{"type":"string"},"name":{"type":"string"},"persona":{"type":"string"},"provider":{"type":"string","enum":["claude","codex"]},"skills":{"type":"array","items":{"type":"string"}},"repo_access":{"type":["array","null"],"items":{"type":"string"}},"connections":{"type":"array","items":{"type":"string"}}},"required":["id"]}},
{"name":"create_skill","description":"Create a skill.","inputSchema":{"type":"object","properties":{"name":{"type":"string"},"content":{"type":"string"}},"required":["name","content"]}},
{"name":"edit_skill","description":"Edit a skill.","inputSchema":{"type":"object","properties":{"id":{"type":"string"},"name":{"type":"string"},"content":{"type":"string"}},"required":["id"]}},
{"name":"register_repo","description":"Register a local Git repository.","inputSchema":{"type":"object","properties":{"name":{"type":"string"},"path":{"type":"string"}},"required":["name","path"]}},
{"name":"attach_connection","description":"Attach a connected account to a Bot.","inputSchema":{"type":"object","properties":{"bot_id":{"type":"string"},"connection_id":{"type":"string"}},"required":["bot_id","connection_id"]}}
]`

func isPlatformTool(name string) bool {
	return slices.Contains([]string{
		provider.ToolCreateBot, provider.ToolEditBot, provider.ToolCreateSkill,
		provider.ToolEditSkill, provider.ToolRegisterRepo, provider.ToolAttachConnection,
	}, name)
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
	var before, after any
	var entity, id, action string
	action = store.ActionEdit
	switch name {
	case provider.ToolCreateBot:
		var b store.Bot
		if json.Unmarshal(args, &b) != nil || !validBot(b) {
			return nil, &rpcError{Code: _invalidParams, Message: "invalid bot"}
		}
		b.CanManagePlatform = false
		b, err = a.st.CreateBot(ctx, b)
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
			b.RepoAccess = slices.Clone(prev.RepoAccess)
			if json.Unmarshal(args, &b) != nil {
				return nil, &rpcError{Code: _invalidParams, Message: "invalid bot"}
			}
			b.ID, b.CanManagePlatform = in.ID, prev.CanManagePlatform
			if !validBot(b) {
				return nil, &rpcError{Code: _invalidParams, Message: "invalid bot"}
			}
			b, err = a.st.UpdateBot(ctx, b)
		}
		after, entity, id = b, store.EntityBot, in.ID
	case provider.ToolCreateSkill:
		var sk store.Skill
		if json.Unmarshal(args, &sk) != nil || sk.Name == "" {
			return nil, &rpcError{Code: _invalidParams, Message: "invalid skill"}
		}
		sk, err = a.st.CreateSkill(ctx, sk.Name, sk.Content)
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
			if json.Unmarshal(args, &sk) != nil || sk.Name == "" {
				return nil, &rpcError{Code: _invalidParams, Message: "invalid skill"}
			}
			sk.ID = in.ID
			sk, err = a.st.UpdateSkill(ctx, sk)
		}
		after, entity, id = sk, store.EntitySkill, in.ID
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
	return a.jsonText(after)
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
