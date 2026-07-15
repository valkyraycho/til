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
