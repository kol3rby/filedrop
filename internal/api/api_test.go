package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"filedrop/internal/blob"
	"filedrop/internal/config"
	"filedrop/internal/store"
)

type env struct {
	srv      *httptest.Server
	app      *Server
	blobsDir string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(context.Background(), filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	blobsDir := filepath.Join(dir, "blobs")
	bl, err := blob.New(blobsDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{MaxUploadSize: 1 << 10, SlugLength: 8}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	app := New(cfg, st, bl, log)
	srv := httptest.NewServer(app.Handler())
	t.Cleanup(srv.Close)
	return &env{srv: srv, app: app, blobsDir: blobsDir}
}

type resp struct {
	status int
	header http.Header
	body   string
}

func (e *env) do(t *testing.T, method, path, body string, headers ...string) resp {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, e.srv.URL+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return resp{res.StatusCode, res.Header, string(b)}
}

func expect(t *testing.T, r resp, status int) {
	t.Helper()
	if r.status != status {
		t.Fatalf("status = %d, want %d; body = %q", r.status, status, r.body)
	}
}

func (e *env) blobCount(t *testing.T) int {
	entries, err := os.ReadDir(e.blobsDir)
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

func TestUploadAndVersioning(t *testing.T) {
	e := newEnv(t)

	r := e.do(t, "POST", "/myslug/notes.txt", "v1 content", "Content-Type", "application/x-www-form-urlencoded")
	expect(t, r, 201)
	want := e.srv.URL + "/myslug/notes.txt"
	if r.body != want+"\n" || r.header.Get("Location") != want || r.header.Get("X-File-Version") != "1" {
		t.Fatalf("upload response: body %q, headers %v", r.body, r.header)
	}

	r = e.do(t, "POST", "/myslug/notes.txt", "v2 content", "Accept", "application/json")
	expect(t, r, 201)
	var got struct {
		Version     int
		ContentType string `json:"content_type"`
		URL         string
		VersionURL  string `json:"version_url"`
	}
	if err := json.Unmarshal([]byte(r.body), &got); err != nil {
		t.Fatal(err)
	}
	if got.Version != 2 || got.URL != want || got.VersionURL != want+"?version=2" || got.ContentType != "text/plain; charset=utf-8" {
		t.Fatalf("json upload = %+v", got)
	}

	expect(t, e.do(t, "GET", "/myslug/notes.txt", ""), 200)
	if r := e.do(t, "GET", "/myslug/notes.txt", ""); r.body != "v2 content" || r.header.Get("X-File-Version") != "2" {
		t.Fatalf("latest = %q", r.body)
	}
	if r := e.do(t, "GET", "/myslug/notes.txt?version=1", ""); r.body != "v1 content" {
		t.Fatalf("v1 = %q", r.body)
	}
	if e.blobCount(t) != 2 {
		t.Fatalf("blobs = %d", e.blobCount(t))
	}
}

func TestUploadAuto(t *testing.T) {
	e := newEnv(t)

	r := e.do(t, "POST", "/?filename=report.pdf", "%PDF-1.4 data")
	expect(t, r, 201)
	url := strings.TrimSpace(r.body)
	if !strings.HasPrefix(url, e.srv.URL+"/") || !strings.HasSuffix(url, "/report.pdf") {
		t.Fatalf("url = %q", url)
	}
	path := strings.TrimPrefix(url, e.srv.URL)
	if r := e.do(t, "GET", path, ""); r.body != "%PDF-1.4 data" || r.header.Get("Content-Type") != "application/pdf" {
		t.Fatalf("download = %q %q", r.body, r.header.Get("Content-Type"))
	}

	r = e.do(t, "POST", "/", "hello", "X-Filename", "hello.txt")
	expect(t, r, 201)
	if !strings.HasSuffix(strings.TrimSpace(r.body), "/hello.txt") || strings.TrimSpace(r.body) == url {
		t.Fatalf("x-filename url = %q", r.body)
	}

	r = e.do(t, "POST", "/", "plain text")
	expect(t, r, 201)
	if !regexp.MustCompile(`/file_\d{4}-\d{2}-\d{2}_\d{2}-\d{2}-\d{2}\.txt\n$`).MatchString(r.body) {
		t.Fatalf("generated name url = %q", r.body)
	}
}

func TestUploadToSlug(t *testing.T) {
	e := newEnv(t)

	r := e.do(t, "POST", "/s?filename=r%C3%A9sum%C3%A9%20final.txt", "one")
	expect(t, r, 201)
	if want := e.srv.URL + "/s/r%C3%A9sum%C3%A9%20final.txt\n"; r.body != want {
		t.Fatalf("url = %q, want %q", r.body, want)
	}
	r = e.do(t, "POST", "/s/", "two", "X-Filename", "résumé final.txt")
	expect(t, r, 201)
	if r.header.Get("X-File-Version") != "2" {
		t.Fatalf("version = %q", r.header.Get("X-File-Version"))
	}
	r = e.do(t, "POST", "/s", "plain text")
	expect(t, r, 201)
	if !regexp.MustCompile(`/s/file_\d{4}-\d{2}-\d{2}_\d{2}-\d{2}-\d{2}\.txt\n$`).MatchString(r.body) {
		t.Fatalf("generated name url = %q", r.body)
	}

	expect(t, e.do(t, "POST", "/bad.slug?filename=a.txt", "x"), 400)
	expect(t, e.do(t, "POST", "/healthz?filename=a.txt", "x"), 400)
	expect(t, e.do(t, "POST", "/s?filename=..", "x"), 400)
	expect(t, e.do(t, "POST", "/s?filename=a.txt", ""), 400)
}

func TestUploadErrors(t *testing.T) {
	e := newEnv(t)
	expect(t, e.do(t, "POST", "/bad.slug/a.txt", "x"), 400)
	expect(t, e.do(t, "POST", "/healthz/a.txt", "x"), 400)
	// "/s/.." is cleaned to "/" by the router (redirect), so ".." never reaches a handler.
	expect(t, e.do(t, "POST", "/s/%2E%2E", "x"), 400)
	expect(t, e.do(t, "POST", "/s/a%2Fb", "x"), 400)
	expect(t, e.do(t, "POST", "/s/a%5Cb", "x"), 400)
	expect(t, e.do(t, "POST", "/?filename=..%2Fx", "x"), 400)
	expect(t, e.do(t, "POST", "/s/a.txt", ""), 400)
	expect(t, e.do(t, "POST", "/s/a.txt", strings.Repeat("x", 2000)), 413)

	r := e.do(t, "POST", "/s/a.txt", "", "Accept", "application/json")
	if r.status != 400 || !strings.Contains(r.body, `"error"`) {
		t.Fatalf("json error = %d %q", r.status, r.body)
	}
	if e.blobCount(t) != 0 {
		t.Fatalf("failed uploads left %d files", e.blobCount(t))
	}
}

func TestDownload(t *testing.T) {
	e := newEnv(t)
	expect(t, e.do(t, "POST", "/s/r%C3%A9sum%C3%A9%20final.txt", "0123456789"), 201)

	r := e.do(t, "GET", "/s/r%C3%A9sum%C3%A9%20final.txt", "")
	expect(t, r, 200)
	cd := r.header.Get("Content-Disposition")
	if cd != `attachment; filename="r_sum_ final.txt"; filename*=UTF-8''r%C3%A9sum%C3%A9%20final.txt` {
		t.Fatalf("content-disposition = %q", cd)
	}
	if r.header.Get("ETag") == "" || r.header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("headers = %v", r.header)
	}

	r = e.do(t, "GET", "/s/r%C3%A9sum%C3%A9%20final.txt", "", "Range", "bytes=2-4")
	if r.status != 206 || r.body != "234" {
		t.Fatalf("range = %d %q", r.status, r.body)
	}
	r = e.do(t, "GET", "/s/r%C3%A9sum%C3%A9%20final.txt", "", "If-None-Match", r.header.Get("ETag"))
	expect(t, r, 304)

	expect(t, e.do(t, "HEAD", "/s/r%C3%A9sum%C3%A9%20final.txt", ""), 200)
	expect(t, e.do(t, "GET", "/s/missing.txt", ""), 404)
	expect(t, e.do(t, "GET", "/s/r%C3%A9sum%C3%A9%20final.txt?version=5", ""), 404)
	expect(t, e.do(t, "GET", "/s/r%C3%A9sum%C3%A9%20final.txt?version=abc", ""), 400)
	expect(t, e.do(t, "GET", "/s/r%C3%A9sum%C3%A9%20final.txt?version=0", ""), 400)
	expect(t, e.do(t, "PUT", "/s/x.txt", "x"), 405)
}

func TestDelete(t *testing.T) {
	e := newEnv(t)
	for range 3 {
		expect(t, e.do(t, "POST", "/s/a.txt", "data"), 201)
	}
	expect(t, e.do(t, "POST", "/s/b.txt", "data"), 201)

	expect(t, e.do(t, "DELETE", "/s/a.txt?version=2", ""), 204)
	expect(t, e.do(t, "GET", "/s/a.txt?version=2", ""), 404)
	expect(t, e.do(t, "GET", "/s/a.txt?version=1", ""), 200)
	if e.blobCount(t) != 3 {
		t.Fatalf("blobs after version delete = %d", e.blobCount(t))
	}

	expect(t, e.do(t, "DELETE", "/s/a.txt", ""), 204)
	expect(t, e.do(t, "GET", "/s/a.txt", ""), 404)
	expect(t, e.do(t, "DELETE", "/s/a.txt", ""), 404)
	if e.blobCount(t) != 1 {
		t.Fatalf("blobs after file delete = %d", e.blobCount(t))
	}
	expect(t, e.do(t, "GET", "/s/b.txt", ""), 200)
}

func TestList(t *testing.T) {
	e := newEnv(t)
	expect(t, e.do(t, "POST", "/s/a.txt", "one"), 201)
	expect(t, e.do(t, "POST", "/s/a.txt", "two"), 201)
	expect(t, e.do(t, "POST", "/s/<b>.txt", "three"), 201)

	r := e.do(t, "GET", "/s", "")
	expect(t, r, 200)
	lines := strings.Split(strings.TrimSpace(r.body), "\n")
	if len(lines) != 4 || !strings.HasPrefix(lines[0], "FILENAME") || !strings.Contains(lines[2], "a.txt") || !strings.Contains(lines[2], "v2") {
		t.Fatalf("text list:\n%s", r.body)
	}

	r = e.do(t, "GET", "/s/", "", "Accept", "application/json")
	expect(t, r, 200)
	var files []struct {
		Filename string
		Version  int
		URL      string
	}
	if err := json.Unmarshal([]byte(r.body), &files); err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 || files[0].URL != e.srv.URL+"/s/%3Cb%3E.txt?version=1" || files[1].Filename != "a.txt" || files[1].Version != 2 {
		t.Fatalf("json list = %+v", files)
	}

	r = e.do(t, "GET", "/s", "", "Accept", "text/html")
	expect(t, r, 200)
	if !strings.Contains(r.body, "&lt;b&gt;.txt") || strings.Contains(r.body, "<b>.txt") ||
		!strings.Contains(r.body, `data-url="/s/a.txt?version=2"`) ||
		!strings.Contains(r.body, `id="dropzone" data-url="/s" data-max="1024"`) {
		t.Fatalf("html list:\n%s", r.body)
	}

	expect(t, e.do(t, "GET", "/unknown", ""), 404)
	expect(t, e.do(t, "GET", "/favicon.ico", ""), 404)
	r = e.do(t, "GET", "/unknown", "", "Accept", "text/html")
	if r.status != 404 || !strings.Contains(r.body, "No file in this slug") || !strings.Contains(r.body, `id="dropzone"`) {
		t.Fatalf("html empty list = %d", r.status)
	}
	if r = e.do(t, "GET", "/bad.slug", "", "Accept", "text/html"); strings.Contains(r.body, `id="dropzone"`) {
		t.Fatal("upload zone shown for an invalid slug")
	}
}

func TestDocs(t *testing.T) {
	e := newEnv(t)

	r := e.do(t, "GET", "/", "")
	expect(t, r, 200)
	if !strings.HasPrefix(r.header.Get("Content-Type"), "text/markdown") || !strings.HasPrefix(r.body, "# filedrop") {
		t.Fatalf("markdown docs: %q", r.header.Get("Content-Type"))
	}
	if strings.Contains(r.body, "{{") || !strings.Contains(r.body, e.srv.URL+"/k3Fz9QpA") || !strings.Contains(r.body, "1 KB") {
		t.Fatal("placeholders not substituted")
	}

	r = e.do(t, "GET", "/", "", "Accept", "text/html,application/xhtml+xml")
	expect(t, r, 200)
	if !strings.HasPrefix(r.header.Get("Content-Type"), "text/html") || !strings.Contains(r.body, "<table>") || !strings.Contains(r.body, "<h1") {
		t.Fatalf("html docs:\n%.300s", r.body)
	}

	if r := e.do(t, "GET", "/?format=html", ""); !strings.Contains(r.body, "<table>") {
		t.Fatal("format=html ignored")
	}
	if r := e.do(t, "GET", "/?format=md", "", "Accept", "text/html"); !strings.HasPrefix(r.body, "# filedrop") {
		t.Fatal("format=md ignored")
	}
	expect(t, e.do(t, "GET", "/?format=pdf", ""), 400)
	expect(t, e.do(t, "GET", "/healthz", ""), 200)
}

func TestBaseURLFromProxyHeaders(t *testing.T) {
	e := newEnv(t)
	r := e.do(t, "POST", "/s/a.txt", "x", "X-Forwarded-Proto", "https", "X-Forwarded-Host", "files.example.com")
	if r.body != "https://files.example.com/s/a.txt\n" {
		t.Fatalf("url = %q", r.body)
	}
}

func TestSetConfig(t *testing.T) {
	e := newEnv(t)
	body := strings.Repeat("x", 2000)
	expect(t, e.do(t, "POST", "/s/a.txt", body), 413)

	e.app.SetConfig(config.Config{MaxUploadSize: 1 << 20, SlugLength: 8, BaseURL: "https://files.example.com"})
	r := e.do(t, "POST", "/s/a.txt", body)
	expect(t, r, 201)
	if want := "https://files.example.com/s/a.txt"; !strings.Contains(r.body, want) {
		t.Errorf("body = %q, want it to contain %q", r.body, want)
	}
}
