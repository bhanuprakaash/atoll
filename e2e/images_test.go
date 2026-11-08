package e2e

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type (
	imageIn struct {
		Mime string `json:"mime"`
		Data string `json:"data"`
	}

	attachment struct {
		ID   string `json:"id"`
		Mime string `json:"mime"`
	}

	imageMessage struct {
		ID          string       `json:"id"`
		Role        string       `json:"role"`
		Text        string       `json:"text"`
		Attachments []attachment `json:"attachments"`
	}
)

func tinyPNG(t *testing.T, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for x := range 4 {
		for y := range 4 {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func tinyJPEG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, 4, 4)), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func tinyGIF(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := gif.Encode(&b, image.NewRGBA(image.Rect(0, 0, 4, 4)), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func asImage(mime string, b []byte) imageIn {
	return imageIn{Mime: mime, Data: base64.StdEncoding.EncodeToString(b)}
}

// sendImages posts a message with images and returns the user message the API returned.
func (s *server) sendImages(t *testing.T, th thread, text string, images []imageIn, want int) imageMessage {
	t.Helper()
	var out struct {
		Message imageMessage `json:"message"`
	}
	s.do(t, "POST", "/api/threads/"+th.ID+"/messages", map[string]any{"text": text, "images": images}, &out, want)
	return out.Message
}

func (s *server) imageMessages(t *testing.T, th thread) []imageMessage {
	t.Helper()
	var ms []imageMessage
	s.do(t, "GET", "/api/threads/"+th.ID+"/messages", nil, &ms, http.StatusOK)
	return ms
}

// get returns the status, Content-Type and body of a GET.
func (s *server) get(t *testing.T, path string) (int, string, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), "GET", s.url+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header.Get("Content-Type"), b
}

func (s *server) wantImage(t *testing.T, path, mime string, want []byte) {
	t.Helper()
	status, ct, body := s.get(t, path)
	if status != http.StatusOK || ct != mime || !bytes.Equal(body, want) {
		t.Errorf("GET %s: status %d, Content-Type %q, %d bytes; want 200, %q, the %d sent bytes", path, status, ct, len(body), mime, len(want))
	}
}

func TestImages_SendStoreAndServe(t *testing.T) {
	m, s := start(t, "claude")
	b := s.newBot(t, "Pixel", "claude")
	th := s.newThread(t, b)
	red, green, jpg := tinyPNG(t, color.RGBA{R: 255, A: 255}), tinyPNG(t, color.RGBA{G: 255, A: 255}), tinyJPEG(t)

	ev := s.events(t, th)
	msg := s.sendImages(t, th, _keepPrompt, []imageIn{asImage("image/png", red), asImage("image/jpeg", jpg)}, http.StatusAccepted)
	s.finished(t, ev, s.runOf(t, th))
	if len(msg.Attachments) != 2 || msg.Attachments[0].Mime != "image/png" || msg.Attachments[1].Mime != "image/jpeg" {
		t.Fatalf("returned attachments %+v, want [image/png, image/jpeg] with ids", msg.Attachments)
	}
	s.wantImage(t, "/api/attachments/"+msg.Attachments[0].ID, "image/png", red)
	s.wantImage(t, "/api/attachments/"+msg.Attachments[1].ID, "image/jpeg", jpg)
	if status, _, _ := s.get(t, "/api/attachments/no-such-attachment"); status != http.StatusNotFound {
		t.Errorf("GET unknown attachment: status %d, want 404", status)
	}

	s.kill()
	s = m.serve(t)
	listed := s.imageMessages(t, th)
	if len(listed) != 2 || listed[0].Role != "user" || !reflect.DeepEqual(listed[0].Attachments, msg.Attachments) || len(listed[1].Attachments) != 0 {
		t.Errorf("messages after restart %+v, want the user message with attachments %+v, then a reply with none", listed, msg.Attachments)
	}
	s.wantImage(t, "/api/attachments/"+msg.Attachments[0].ID, "image/png", red)

	// Images with no text are accepted, on both routes.
	only := s.sendImages(t, th, "", []imageIn{asImage("image/png", green)}, http.StatusAccepted)
	if len(only.Attachments) != 1 {
		t.Errorf("images-only message attachments %+v, want one", only.Attachments)
	}
	var created struct {
		Message imageMessage `json:"message"`
	}
	s.do(t, "POST", "/api/bots/"+b.ID+"/threads", map[string]any{"text": _keepPrompt, "images": []imageIn{asImage("image/gif", tinyGIF(t))}}, &created, http.StatusCreated)
	if len(created.Message.Attachments) != 1 || created.Message.Attachments[0].Mime != "image/gif" {
		t.Errorf("new thread message attachments %+v, want one image/gif", created.Message.Attachments)
	}
}

// runOf returns the id of the latest run of a thread.
func (s *server) runOf(t *testing.T, th thread) string {
	t.Helper()
	var ms []message
	s.do(t, "GET", "/api/threads/"+th.ID+"/messages", nil, &ms, http.StatusOK)
	for i := len(ms) - 1; i >= 0; i-- {
		if ms[i].RunID != "" {
			return ms[i].RunID
		}
	}
	t.Fatalf("thread %s has no message with a run: %+v", th.ID, ms)
	return ""
}

func TestImages_Rejected(t *testing.T) {
	_, s := start(t, "claude")
	b := s.newBot(t, "Strict", "claude")
	small := tinyPNG(t, color.RGBA{B: 255, A: 255})
	many := make([]imageIn, 11)
	for i := range many {
		many[i] = asImage("image/png", small)
	}
	for _, c := range []struct {
		name   string
		text   string
		images []imageIn
	}{
		{"empty text and no images", "", nil},
		{"mime not allowed", "look", []imageIn{asImage("image/svg+xml", []byte("<svg/>"))}},
		{"image over 5 MB", "look", []imageIn{asImage("image/png", make([]byte, 5*1024*1024+1))}},
		{"eleven images", "look", many},
	} {
		t.Run(c.name, func(t *testing.T) {
			th := s.newThread(t, b)
			s.sendImages(t, th, c.text, c.images, http.StatusBadRequest)
			if got := s.imageMessages(t, th); len(got) != 0 {
				t.Errorf("rejected message left %+v, want no messages", got)
			}
			if c.text == "" && c.images == nil {
				return // a thread may start empty; only a message needs text or images
			}
			var before, after []thread
			s.do(t, "GET", "/api/bots/"+b.ID+"/threads", nil, &before, http.StatusOK)
			s.do(t, "POST", "/api/bots/"+b.ID+"/threads", map[string]any{"text": c.text, "images": c.images}, nil, http.StatusBadRequest)
			s.do(t, "GET", "/api/bots/"+b.ID+"/threads", nil, &after, http.StatusOK)
			if len(after) != len(before) {
				t.Errorf("rejected new thread left %d threads, want %d", len(after), len(before))
			}
		})
	}

	ten := many[:10]
	th := s.newThread(t, b)
	if got := s.sendImages(t, th, _keepPrompt, ten, http.StatusAccepted); len(got.Attachments) != 10 {
		t.Errorf("ten images: %d attachments, want 10", len(got.Attachments))
	}
}

func TestImages_ProviderInput(t *testing.T) {
	red, jpg := tinyPNG(t, color.RGBA{R: 255, A: 255}), tinyJPEG(t)
	images := []imageIn{asImage("image/png", red), asImage("image/jpeg", jpg)}
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m, s := start(t, p)
			th := s.newThread(t, s.newBot(t, "Eyes", p))
			ev := s.events(t, th)
			s.sendImages(t, th, _keepPrompt, images, http.StatusAccepted)
			s.finished(t, ev, s.runOf(t, th))

			sessions := m.sessions(t, p)
			var turn string
			for _, l := range sessions[len(sessions)-1].stdin {
				if isTurn(l) {
					turn = l
				}
			}
			var items []map[string]any
			var want []map[string]any
			if p == "claude" {
				var l struct {
					Message struct {
						Content []map[string]any `json:"content"`
					} `json:"message"`
				}
				if err := json.Unmarshal([]byte(turn), &l); err != nil {
					t.Fatalf("claude user message %s: %v", turn, err)
				}
				items = l.Message.Content
				for _, img := range images {
					want = append(want, map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": img.Mime, "data": img.Data}})
				}
			} else {
				var l struct {
					Params struct {
						Input []map[string]any `json:"input"`
					} `json:"params"`
				}
				if err := json.Unmarshal([]byte(turn), &l); err != nil {
					t.Fatalf("codex turn/start %s: %v", turn, err)
				}
				items = l.Params.Input
				for _, img := range images {
					want = append(want, map[string]any{"type": "image", "url": "data:" + img.Mime + ";base64," + img.Data})
				}
			}
			if len(items) != 3 || items[0]["type"] != "text" || !strings.Contains(items[0]["text"].(string), _keepPrompt) {
				t.Fatalf("%s input %v, want the text item first, then 2 images", p, items)
			}
			if !reflect.DeepEqual(items[1:], want) {
				t.Errorf("%s image items\n got %v\nwant %v", p, items[1:], want)
			}
		})
	}
}

func TestImages_DeleteBot(t *testing.T) {
	_, s := start(t, "claude")
	b := s.newBot(t, "Gone", "claude")
	th := s.newThread(t, b)
	red := tinyPNG(t, color.RGBA{R: 255, A: 255})
	ev := s.events(t, th)
	msg := s.sendImages(t, th, _keepPrompt, []imageIn{asImage("image/png", red)}, http.StatusAccepted)
	s.finished(t, ev, s.runOf(t, th))
	if len(msg.Attachments) != 1 {
		t.Fatalf("returned attachments %+v, want one", msg.Attachments)
	}
	s.wantImage(t, "/api/attachments/"+msg.Attachments[0].ID, "image/png", red)

	s.do(t, "DELETE", "/api/bots/"+b.ID, nil, nil, http.StatusNoContent)

	if status, _, _ := s.get(t, "/api/attachments/"+msg.Attachments[0].ID); status != http.StatusNotFound {
		t.Errorf("GET attachment of a deleted Bot: status %d, want 404", status)
	}
}

func TestImages_ThreadFile(t *testing.T) {
	f := newRepoFixture(t, "claude")
	b := f.bot(t, "claude", "all", nil)
	th, work, _, _ := f.work(t, b, _unfinishedRepoPrompt, "fix/seeded-bug")
	red := tinyPNG(t, color.RGBA{R: 255, A: 255})
	outside := filepath.Join(filepath.Dir(work), "outside.png")
	writeFile(t, filepath.Join(work, "shot.png"), string(red))
	writeFile(t, filepath.Join(work, "docs", "pic.jpeg"), string(tinyJPEG(t)))
	writeFile(t, outside, string(red))
	if err := os.Symlink(outside, filepath.Join(work, "link.png")); err != nil {
		t.Fatal(err)
	}
	file := func(p string) string { return "/api/threads/" + th.ID + "/file?path=" + url.QueryEscape(p) }

	f.s.wantImage(t, file("shot.png"), "image/png", red)
	f.s.wantImage(t, file(filepath.Join(work, "shot.png")), "image/png", red)
	f.s.wantImage(t, file("docs/pic.jpeg"), "image/jpeg", tinyJPEG(t))
	for _, p := range []string{"../outside.png", outside, "link.png", "answer.txt", "missing.png"} {
		if status, _, _ := f.s.get(t, file(p)); status != http.StatusNotFound {
			t.Errorf("GET thread file %q: status %d, want 404", p, status)
		}
	}
}
