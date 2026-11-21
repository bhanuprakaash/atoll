package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
)

// fakeMerge is a PUT /repos/{owner}/{repo}/pulls/{number}/merge the fake GitHub got: the pull
// request, the sha and merge_method of its body, and whether it merged.
type fakeMerge struct {
	N           int
	SHA, Method string
	Merged      bool
}

// pushHead moves the head of pull request n to a new commit, as a push to its branch does.
func (g *fakeGitHub) pushHead(n int) string {
	sha := headSHA(n + 1000)
	g.setHead(n, sha)
	return sha
}

// setConflicts makes pull request n unmergeable (GitHub says mergeable false) or mergeable again.
func (g *fakeGitHub) setConflicts(n int, on bool) {
	g.set(func() { g.pr(n).conflicts = on })
}

// merges is every merge request the fake got for pull request n, in order.
func (g *fakeGitHub) merges(n int) []fakeMerge {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []fakeMerge
	for _, m := range g.watch.merges {
		if m.N == n {
			out = append(out, m)
		}
	}
	return slices.Clone(out)
}

// mergePull is PUT /repos/{owner}/{repo}/pulls/{number}/merge as GitHub has it: 409 when sha is not
// the head commit, 405 when the pull request is not mergeable, else the pull request is merged.
func (g *fakeGitHub) mergePull(w http.ResponseWriter, r *http.Request) {
	if g.reject(w, r) {
		return
	}
	n, _ := strconv.Atoi(r.PathValue("number"))
	var in struct {
		SHA         string `json:"sha"`
		MergeMethod string `json:"merge_method"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	status, reply := http.StatusOK, ""
	g.set(func() {
		pr := g.pr(n)
		merge := fakeMerge{N: n, SHA: in.SHA, Method: in.MergeMethod}
		switch {
		case pr == nil:
			status, reply = http.StatusNotFound, `{"message":"Not Found"}`
		case in.SHA != pr.headOf(n):
			status, reply = http.StatusConflict, `{"message":"Head branch was modified. Review and try the merge again."}`
		case pr.conflicts:
			status, reply = http.StatusMethodNotAllowed, `{"message":"Pull Request is not mergeable"}`
		default:
			pr.state, pr.merged, merge.Merged = "closed", true, true
			reply = fmt.Sprintf(`{"sha":"%s","merged":true,"message":"Pull Request successfully merged"}`, headSHA(n+2000))
		}
		g.watch.merges = append(g.watch.merges, merge)
	})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = fmt.Fprint(w, reply)
}
