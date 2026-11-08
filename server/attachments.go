package server

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"atoll/store"
)

const (
	_maxImageBytes = 5 << 20
	_maxImages     = 10
)

var _imageMimes = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".gif": "image/gif", ".webp": "image/webp",
}

type imageBody struct {
	Mime string `json:"mime"`
	Data string `json:"data"`
}

// decodeImages checks the images of a request and returns them, or a message for a 400.
func decodeImages(in []imageBody) ([]store.Attachment, string) {
	if len(in) > _maxImages {
		return nil, fmt.Sprintf("at most %d images per message", _maxImages)
	}
	out := make([]store.Attachment, 0, len(in))
	for _, img := range in {
		if !isImageMime(img.Mime) {
			return nil, "image type " + img.Mime + " is not allowed"
		}
		if base64.StdEncoding.DecodedLen(len(img.Data)) > _maxImageBytes {
			return nil, "an image is over 5 MB"
		}
		data, err := base64.StdEncoding.DecodeString(img.Data)
		if err != nil {
			return nil, "image data is not valid base64"
		}
		if len(data) > _maxImageBytes {
			return nil, "an image is over 5 MB"
		}
		out = append(out, store.Attachment{Mime: img.Mime, Data: data})
	}
	return out, ""
}

func isImageMime(mime string) bool {
	for _, m := range _imageMimes {
		if m == mime {
			return true
		}
	}
	return false
}

func (a *api) getAttachment(w http.ResponseWriter, r *http.Request) {
	att, err := a.st.Attachment(r.Context(), r.PathValue("id"))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", att.Mime)
	_, _ = w.Write(att.Data)
}

// threadFile serves an image file from inside the thread's work folder.
func (a *api) threadFile(w http.ResponseWriter, r *http.Request) {
	t, err := a.st.Thread(r.Context(), r.PathValue("id"))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	p := r.URL.Query().Get("path")
	mime, ok := _imageMimes[strings.ToLower(filepath.Ext(p))]
	if !ok {
		http.NotFound(w, r)
		return
	}
	root, err := filepath.EvalSymlinks(a.runs.WorkDir(t))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	full, err := filepath.EvalSymlinks(p)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if rel, err := filepath.Rel(root, full); err != nil || rel == ".." ||
		strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		http.NotFound(w, r)
		return
	}
	data, err := os.ReadFile(full)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", mime)
	_, _ = w.Write(data)
}
