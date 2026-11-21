package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
)

// _fakePRAuthor is the user.login of every pull request the fake GitHub holds.
const _fakePRAuthor = "reef-fixture"

// fakeFile is a file of a pull request; an empty Patch is absent from the reply, as GitHub leaves it
// out for a binary or too large file.
type fakeFile struct {
	Path                 string
	Additions, Deletions int
	Patch                string
}

// prRead is the state behind the routes the PR panel reads: the files of a pull request and the
// merge methods of the repo. The caller holds g.mu.
type prRead struct {
	files   map[int][]fakeFile
	queries map[int][]string // the raw query of every request for the files of a pull request
	methods *[3]bool         // allow_merge_commit, allow_squash_merge, allow_rebase_merge; nil: all
}

// setPullFiles sets the files of pull request n.
func (g *fakeGitHub) setPullFiles(n int, files ...fakeFile) {
	g.set(func() {
		if g.read.files == nil {
			g.read.files = map[int][]fakeFile{}
		}
		g.read.files[n] = files
	})
}

// setMergeMethods sets what the repo allows: merge commit, squash and rebase.
func (g *fakeGitHub) setMergeMethods(merge, squash, rebase bool) {
	g.set(func() { g.read.methods = &[3]bool{merge, squash, rebase} })
}

// fileQueries is the raw query of every request for the files of pull request n.
func (g *fakeGitHub) fileQueries(n int) []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Clone(g.read.queries[n])
}

// pullFiles is GET /repos/{owner}/{repo}/pulls/{number}/files. It answers one file a page whatever
// per_page says, with a Link header for the next page, so a reader that does not follow it sees one.
func (g *fakeGitHub) pullFiles(w http.ResponseWriter, r *http.Request) {
	if g.reject(w, r) {
		return
	}
	n, _ := strconv.Atoi(r.PathValue("number"))
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	page = max(page, 1)
	var files []fakeFile
	var found bool
	g.set(func() {
		if g.read.queries == nil {
			g.read.queries = map[int][]string{}
		}
		g.read.queries[n] = append(g.read.queries[n], r.URL.RawQuery)
		found = g.pr(n) != nil
		files = g.read.files[n]
	})
	w.Header().Set("Content-Type", "application/json")
	if !found {
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprint(w, `{"message":"Not Found"}`)
		return
	}
	out := []map[string]any{}
	if page <= len(files) {
		f := files[page-1]
		file := map[string]any{"filename": f.Path, "status": "modified", "additions": f.Additions,
			"deletions": f.Deletions, "changes": f.Additions + f.Deletions}
		if f.Patch != "" {
			file["patch"] = f.Patch
		}
		out = append(out, file)
	}
	if page < len(files) {
		w.Header().Set("Link", fmt.Sprintf(`<%s/repos/%s/%s/pulls/%d/files?per_page=100&page=%d>; rel="next"`,
			"http://"+r.Host, r.PathValue("owner"), r.PathValue("repo"), n, page+1))
	}
	_ = json.NewEncoder(w).Encode(out)
}

// getRepo is GET /repos/{owner}/{repo}: the merge methods the repo allows.
func (g *fakeGitHub) getRepo(w http.ResponseWriter, r *http.Request) {
	if g.reject(w, r) {
		return
	}
	m := [3]bool{true, true, true}
	g.set(func() {
		if g.read.methods != nil {
			m = *g.read.methods
		}
	})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"full_name": r.PathValue("owner") + "/" + r.PathValue("repo"),
		"allow_merge_commit": m[0], "allow_squash_merge": m[1], "allow_rebase_merge": m[2]})
}
