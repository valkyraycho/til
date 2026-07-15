package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/valkyraycho/til/internal/store"
)

const testPort = 4711

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "til.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func get(t *testing.T, h http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Host = fmt.Sprintf("127.0.0.1:%d", testPort)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestIndexShowsRecentEntriesAndTags(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Add("docker networking fix\ndetails", []string{"docker"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	h := New(s, testPort)

	rec := get(t, h, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"docker networking fix", `hx-get="/search"`, "/static/htmx.min.js", ">docker</"} {
		if !strings.Contains(body, want) {
			t.Errorf("index missing %q", want)
		}
	}
}

func TestSearchFragment(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Add("kubernetes ingress note", nil); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := s.Add("postgres vacuum note", nil); err != nil {
		t.Fatalf("Add: %v", err)
	}
	h := New(s, testPort)

	rec := get(t, h, "/search?q=kubernetes")
	body := rec.Body.String()
	if !strings.Contains(body, "kubernetes ingress note") {
		t.Errorf("match missing from fragment: %q", body)
	}
	if strings.Contains(body, "postgres") {
		t.Errorf("non-match leaked into fragment")
	}
}

func TestSearchHostileQueryNoServerError(t *testing.T) {
	s := newTestStore(t)
	h := New(s, testPort)
	for _, q := range []string{`%22`, `AND`, `%28%28%28`, strings.Repeat("x", 2048)} {
		rec := get(t, h, "/search?q="+q)
		if rec.Code != http.StatusOK {
			t.Errorf("q=%s → status %d, want 200", q, rec.Code)
		}
	}
}

func TestSearchBlankQueryWithTag(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Add("tagged note", []string{"go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := s.Add("untagged note", nil); err != nil {
		t.Fatalf("Add: %v", err)
	}
	h := New(s, testPort)

	body := get(t, h, "/search?tag=go").Body.String()
	if !strings.Contains(body, "tagged note") || strings.Contains(body, "untagged note") {
		t.Errorf("tag filter broken: %q", body)
	}
}

func TestSearchPaginationLoadMore(t *testing.T) {
	s := newTestStore(t)
	for i := 0; i < pageSize+1; i++ {
		if _, err := s.Add(fmt.Sprintf("golang note %d", i), nil); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
	h := New(s, testPort)

	page1 := get(t, h, "/search?q=golang").Body.String()
	if !strings.Contains(page1, "offset=50") {
		t.Errorf("first full page missing load-more link")
	}
	page2 := get(t, h, "/search?q=golang&offset=50").Body.String()
	if strings.Contains(page2, "offset=100") {
		t.Errorf("final page should not offer load-more")
	}
}

func TestEntryDetailRendersMarkdown(t *testing.T) {
	s := newTestStore(t)
	e, err := s.Add("## Heading\n\n`code` and <script>alert(1)</script>", nil)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	h := New(s, testPort)

	body := get(t, h, fmt.Sprintf("/entries/%d", e.ID)).Body.String()
	if !strings.Contains(body, "<h2>Heading</h2>") {
		t.Errorf("markdown not rendered: %q", body)
	}
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Errorf("raw script tag leaked into detail view")
	}
}

func TestEntryNotFoundAndBadID(t *testing.T) {
	s := newTestStore(t)
	h := New(s, testPort)
	if code := get(t, h, "/entries/999").Code; code != http.StatusNotFound {
		t.Errorf("missing entry → %d, want 404", code)
	}
	if code := get(t, h, "/entries/abc").Code; code != http.StatusBadRequest {
		t.Errorf("non-numeric id → %d, want 400", code)
	}
}

func TestSecurityHeaders(t *testing.T) {
	s := newTestStore(t)
	h := New(s, testPort)
	rec := get(t, h, "/")
	want := map[string]string{
		"Content-Security-Policy": "default-src 'self'",
		"X-Content-Type-Options":  "nosniff",
		"Referrer-Policy":         "no-referrer",
		"Cache-Control":           "no-store",
	}
	for k, v := range want {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
}

func postForm(t *testing.T, h http.Handler, target, form string, origin string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form))
	req.Host = fmt.Sprintf("127.0.0.1:%d", testPort)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestCreateEntryFromWeb(t *testing.T) {
	s := newTestStore(t)
	h := New(s, testPort)

	rec := postForm(t, h, "/entries", "body=web-born+note+about+caddy&tags=Web,web", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("create → %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "web-born note about caddy") {
		t.Errorf("fresh rows missing new entry: %q", body)
	}
	if !strings.Contains(body, `hx-swap-oob="true"`) || !strings.Contains(body, `id="composer"`) {
		t.Errorf("response missing OOB composer reset")
	}
	got, err := s.Search("caddy", "", 50, 0)
	if err != nil || len(got) != 1 {
		t.Fatalf("entry not persisted: %v %v", got, err)
	}
	if len(got[0].Tags) != 1 || got[0].Tags[0] != "web" {
		t.Errorf("tags = %v, want deduped [web]", got[0].Tags)
	}
}

func TestCreateEmptyBodyRejected(t *testing.T) {
	s := newTestStore(t)
	h := New(s, testPort)
	if code := postForm(t, h, "/entries", "body=++&tags=", "").Code; code != http.StatusBadRequest {
		t.Errorf("blank create → %d, want 400", code)
	}
}

func TestUpdateEntryFromWeb(t *testing.T) {
	s := newTestStore(t)
	e, err := s.Add("original words", []string{"old"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	h := New(s, testPort)

	rec := postForm(t, h, fmt.Sprintf("/entries/%d", e.ID), "body=%23+rewritten+headline&tags=fresh", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("update → %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "<h1>rewritten headline</h1>") {
		t.Errorf("update response missing rendered detail: %q", rec.Body.String())
	}
	got, err := s.Get(e.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Tags) != 1 || got.Tags[0] != "fresh" {
		t.Errorf("tags after update = %v, want [fresh]", got.Tags)
	}
	if code := postForm(t, h, "/entries/999", "body=x", "").Code; code != http.StatusNotFound {
		t.Errorf("update missing entry → want 404")
	}
}

func TestDeleteEntryFromWeb(t *testing.T) {
	s := newTestStore(t)
	e, err := s.Add("doomed from the web", nil)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	h := New(s, testPort)

	target := fmt.Sprintf("/entries/%d/delete", e.ID)
	if code := postForm(t, h, target, "", "").Code; code != http.StatusOK {
		t.Fatalf("delete → want 200")
	}
	if _, err := s.Get(e.ID); err == nil {
		t.Errorf("entry survived web delete")
	}
	if code := postForm(t, h, target, "", "").Code; code != http.StatusNotFound {
		t.Errorf("second delete → want 404")
	}
}

func TestEditFormPrefilled(t *testing.T) {
	s := newTestStore(t)
	e, err := s.Add("editable body text", []string{"go", "sqlite"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	h := New(s, testPort)

	body := get(t, h, fmt.Sprintf("/entries/%d/edit", e.ID)).Body.String()
	if !strings.Contains(body, "editable body text") {
		t.Errorf("edit form missing body: %q", body)
	}
	if !strings.Contains(body, `value="go, sqlite"`) {
		t.Errorf("edit form missing joined tags: %q", body)
	}
}

func TestRowEndpoint(t *testing.T) {
	s := newTestStore(t)
	e, err := s.Add("collapsible note", nil)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	h := New(s, testPort)

	body := get(t, h, fmt.Sprintf("/entries/%d/row", e.ID)).Body.String()
	if !strings.Contains(body, "collapsible note") || !strings.Contains(body, `class="row"`) {
		t.Errorf("row fragment = %q", body)
	}
}

func TestCSRFOriginCheck(t *testing.T) {
	s := newTestStore(t)
	h := New(s, testPort)

	if code := postForm(t, h, "/entries", "body=evil", "https://evil.example.com").Code; code != http.StatusForbidden {
		t.Errorf("foreign Origin POST → %d, want 403", code)
	}
	own := fmt.Sprintf("http://127.0.0.1:%d", testPort)
	if code := postForm(t, h, "/entries", "body=fine", own).Code; code != http.StatusOK {
		t.Errorf("same-origin POST → want 200")
	}
	if code := postForm(t, h, "/entries", "body=also+fine", "").Code; code != http.StatusOK {
		t.Errorf("no-Origin POST (curl) → want 200")
	}
	if code := get(t, h, "/search?q=x").Code; code != http.StatusOK {
		t.Errorf("GET unaffected by origin check → want 200")
	}
}

func TestHostValidationBlocksDNSRebinding(t *testing.T) {
	s := newTestStore(t)
	h := New(s, testPort)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "evil.example.com:4711"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("foreign Host → %d, want 403", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = fmt.Sprintf("localhost:%d", testPort)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("localhost Host → %d, want 200", rec.Code)
	}
}
