package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "til.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func mustAdd(t *testing.T, s *Store, body string, tags ...string) Entry {
	t.Helper()
	e, err := s.Add(body, tags)
	if err != nil {
		t.Fatalf("Add(%q): %v", body, err)
	}
	return e
}

func TestAddAndGet(t *testing.T) {
	s := newTestStore(t)
	added := mustAdd(t, s, "docker DNS fix\nuse 8.8.8.8", "docker", "dns")

	got, err := s.Get(added.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Body != "docker DNS fix\nuse 8.8.8.8" {
		t.Errorf("body = %q", got.Body)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "dns" || got.Tags[1] != "docker" {
		t.Errorf("tags = %v, want [dns docker] (sorted)", got.Tags)
	}
	if got.CreatedAt.IsZero() || got.CreatedAt.Location() != time.UTC {
		t.Errorf("created_at = %v, want non-zero UTC", got.CreatedAt)
	}
}

func TestAddEmptyBodyRejected(t *testing.T) {
	s := newTestStore(t)
	for _, body := range []string{"", "   ", "\n\t"} {
		if _, err := s.Add(body, nil); err == nil {
			t.Errorf("Add(%q) succeeded, want error", body)
		}
	}
}

func TestNormalizeTags(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"lowercase", []string{"Go", "DNS"}, []string{"go", "dns"}},
		{"trim", []string{" docker "}, []string{"docker"}},
		{"drop empty", []string{"", "  ", "go"}, []string{"go"}},
		{"dedupe after normalize", []string{"Go", "go", "GO"}, []string{"go"}},
		{"internal whitespace to dash", []string{"dns stuff"}, []string{"dns-stuff"}},
		{"nil", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeTags(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("got %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestDuplicateTagsDoNotViolatePK(t *testing.T) {
	s := newTestStore(t)
	e := mustAdd(t, s, "note", "Go", "go", "GO")
	got, err := s.Get(e.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Tags) != 1 || got.Tags[0] != "go" {
		t.Errorf("tags = %v, want [go]", got.Tags)
	}
}

func TestSearchStemming(t *testing.T) {
	s := newTestStore(t)
	mustAdd(t, s, "deploying the service to prod")

	got, err := s.Search("deploy", "", 50, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("porter stemming: got %d results for 'deploy', want 1", len(got))
	}
}

func TestSearchPrefix(t *testing.T) {
	s := newTestStore(t)
	mustAdd(t, s, "docker compose networking")

	got, err := s.Search("dock", "", 50, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("prefix match: got %d results for 'dock', want 1", len(got))
	}
}

func TestSearchHostileInputs(t *testing.T) {
	s := newTestStore(t)
	mustAdd(t, s, "some note about sqlite")

	hostile := []string{
		`"`, `""`, `AND`, `OR`, `NOT`, `-`, `(`, `(((`, `)`, `*`,
		`"unclosed`, `a AND b OR (c`, `col:val`, `^first`,
		strings.Repeat("x", 10*1024),
		strings.Repeat("日本語テスト", 100),
	}
	for _, q := range hostile {
		if _, err := s.Search(q, "", 50, 0); err != nil {
			t.Errorf("Search(%.20q) returned error: %v", q, err)
		}
	}
}

func TestSearchSemantics(t *testing.T) {
	s := newTestStore(t)
	a := mustAdd(t, s, "kubernetes ingress note", "k8s")
	b := mustAdd(t, s, "kubernetes secrets note", "k8s", "security")
	c := mustAdd(t, s, "unrelated postgres note", "db")

	t.Run("blank query no tag returns recent id desc", func(t *testing.T) {
		got, err := s.Search("", "", 50, 0)
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if len(got) != 3 || got[0].ID != c.ID || got[1].ID != b.ID || got[2].ID != a.ID {
			t.Errorf("got %d results, order %v", len(got), ids(got))
		}
	})
	t.Run("blank query with tag filters by tag", func(t *testing.T) {
		got, err := s.Search("", "k8s", 50, 0)
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if len(got) != 2 || got[0].ID != b.ID || got[1].ID != a.ID {
			t.Errorf("got %v, want [b a]", ids(got))
		}
	})
	t.Run("query with tag intersects", func(t *testing.T) {
		got, err := s.Search("kubernetes", "security", 50, 0)
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if len(got) != 1 || got[0].ID != b.ID {
			t.Errorf("got %v, want [b]", ids(got))
		}
	})
	t.Run("query alone matches fts", func(t *testing.T) {
		got, err := s.Search("postgres", "", 50, 0)
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if len(got) != 1 || got[0].ID != c.ID {
			t.Errorf("got %v, want [c]", ids(got))
		}
	})
}

func TestSearchPagination(t *testing.T) {
	s := newTestStore(t)
	for i := 0; i < 60; i++ {
		mustAdd(t, s, fmt.Sprintf("note number %d about golang", i))
	}
	page1, err := s.Search("golang", "", 50, 0)
	if err != nil {
		t.Fatalf("Search page1: %v", err)
	}
	page2, err := s.Search("golang", "", 50, 50)
	if err != nil {
		t.Fatalf("Search page2: %v", err)
	}
	if len(page1) != 50 || len(page2) != 10 {
		t.Errorf("pages = %d, %d; want 50, 10", len(page1), len(page2))
	}
}

func TestDeleteSyncsFTS(t *testing.T) {
	s := newTestStore(t)
	e := mustAdd(t, s, "ephemeral wisdom")
	if err := s.Delete(e.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got, err := s.Search("ephemeral", "", 50, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("deleted entry still in FTS index")
	}
	if _, err := s.Get(e.ID); err == nil {
		t.Errorf("Get after delete succeeded, want error")
	}
}

func TestUpdateBodySyncsFTS(t *testing.T) {
	s := newTestStore(t)
	e := mustAdd(t, s, "original zebra content")
	if err := s.UpdateBody(e.ID, "replacement yak content"); err != nil {
		t.Fatalf("UpdateBody: %v", err)
	}
	if got, _ := s.Search("zebra", "", 50, 0); len(got) != 0 {
		t.Errorf("old term still matches after update")
	}
	got, err := s.Search("yak", "", 50, 0)
	if err != nil || len(got) != 1 {
		t.Errorf("new term not found after update: %v, %v", ids(got), err)
	}
}

func TestRowidNotReusedAfterDelete(t *testing.T) {
	s := newTestStore(t)
	mustAdd(t, s, "first")
	b := mustAdd(t, s, "second highest")
	if err := s.Delete(b.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	c := mustAdd(t, s, "third")
	if c.ID <= b.ID {
		t.Errorf("id reused: new id %d <= deleted id %d", c.ID, b.ID)
	}
}

func TestFTSMappingAfterDeleteHighestThenAdd(t *testing.T) {
	s := newTestStore(t)
	mustAdd(t, s, "keeper note about redis")
	high := mustAdd(t, s, "doomed note about kafka")
	if err := s.Delete(high.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	fresh := mustAdd(t, s, "fresh note about nats")

	got, err := s.Search("nats", "", 50, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 || got[0].ID != fresh.ID || !strings.Contains(got[0].Body, "nats") {
		t.Errorf("FTS mapping wrong after delete-highest+add: %+v", got)
	}
	if got, _ := s.Search("kafka", "", 50, 0); len(got) != 0 {
		t.Errorf("deleted note still findable")
	}
}

func TestTagsList(t *testing.T) {
	s := newTestStore(t)
	mustAdd(t, s, "a", "go", "sqlite")
	mustAdd(t, s, "b", "go")
	tags, err := s.Tags()
	if err != nil {
		t.Fatalf("Tags: %v", err)
	}
	if len(tags) != 2 || tags[0] != "go" || tags[1] != "sqlite" {
		t.Errorf("tags = %v, want [go sqlite]", tags)
	}
}

func TestMigrationIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "til.db")
	s1, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	mustAdd(t, s1, "survives reopen")
	s1.Close()

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer s2.Close()
	got, err := s2.Search("survives", "", 50, 0)
	if err != nil || len(got) != 1 {
		t.Errorf("data lost across reopen: %v, %v", ids(got), err)
	}
}

func TestMigrationRejectsNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "til.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := s.db.Exec("PRAGMA user_version = 99"); err != nil {
		t.Fatalf("set user_version: %v", err)
	}
	s.Close()

	if _, err := Open(path); err == nil {
		t.Fatalf("Open succeeded on db with user_version=99, want error")
	}
}

func TestConcurrentFirstRunMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "til.db")
	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s, err := Open(path)
			if err != nil {
				errs[i] = err
				return
			}
			defer s.Close()
			_, errs[i] = s.Add(fmt.Sprintf("migration race %d", i), nil)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: %v", i, err)
		}
	}
}

func TestConcurrentWritersSeparateStores(t *testing.T) {
	path := filepath.Join(t.TempDir(), "til.db")
	s1, err := Open(path)
	if err != nil {
		t.Fatalf("Open s1: %v", err)
	}
	defer s1.Close()
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("Open s2: %v", err)
	}
	defer s2.Close()

	var wg sync.WaitGroup
	errCh := make(chan error, 40)
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			if _, err := s1.Add(fmt.Sprintf("writer one %d", i), nil); err != nil {
				errCh <- err
			}
		}(i)
		go func(i int) {
			defer wg.Done()
			if _, err := s2.Add(fmt.Sprintf("writer two %d", i), nil); err != nil {
				errCh <- err
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("concurrent write failed: %v", err)
	}
	got, err := s1.Search("writer", "", 50, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 40 {
		t.Errorf("got %d entries, want 40", len(got))
	}
}

func TestFKCascadeAcrossPooledConnectionReplacement(t *testing.T) {
	s := newTestStore(t)
	s.db.SetMaxIdleConns(0) // every op gets a fresh connection; pragmas must survive

	e := mustAdd(t, s, "cascade check", "doomed-tag")
	if err := s.Delete(e.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	tags, err := s.Tags()
	if err != nil {
		t.Fatalf("Tags: %v", err)
	}
	if len(tags) != 0 {
		t.Errorf("orphaned tags after cascade on fresh connection: %v", tags)
	}
}

func TestFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permissions not applicable on windows")
	}
	dir := filepath.Join(t.TempDir(), "home", ".til")
	path := filepath.Join(dir, "til.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	di, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if di.Mode().Perm() != 0o700 {
		t.Errorf("dir perm = %o, want 700", di.Mode().Perm())
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat db: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("db perm = %o, want 600", fi.Mode().Perm())
	}
}

func TestCreatedAtFixedWidthUTC(t *testing.T) {
	s := newTestStore(t)
	e := mustAdd(t, s, "timestamp check")
	var raw string
	if err := s.db.QueryRow("SELECT created_at FROM entries WHERE id = ?", e.ID).Scan(&raw); err != nil {
		t.Fatalf("query raw created_at: %v", err)
	}
	if _, err := time.Parse("2006-01-02T15:04:05Z", raw); err != nil {
		t.Errorf("created_at %q is not fixed-width RFC3339 UTC: %v", raw, err)
	}
}

func TestOpenUncreatableDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("/dev/null path semantics are unix-specific")
	}
	if _, err := Open("/dev/null/nope/til.db"); err == nil {
		t.Fatalf("Open under /dev/null succeeded, want error")
	}
}

func TestUpdateAndDeleteMissingEntry(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpdateBody(999, "ghost"); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateBody(999) = %v, want ErrNotFound", err)
	}
	if err := s.Delete(999); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete(999) = %v, want ErrNotFound", err)
	}
	if err := s.UpdateBody(999, "  "); err == nil {
		t.Errorf("UpdateBody with blank body succeeded, want error")
	}
}

func TestCorruptedTimestampSurfacesError(t *testing.T) {
	s := newTestStore(t)
	e := mustAdd(t, s, "soon to be corrupted")
	if _, err := s.db.Exec("UPDATE entries SET created_at = 'garbage' WHERE id = ?", e.ID); err != nil {
		t.Fatalf("corrupt timestamp: %v", err)
	}
	if _, err := s.Get(e.ID); err == nil {
		t.Errorf("Get with corrupt created_at succeeded, want error")
	}
	if _, err := s.Search("", "", 50, 0); err == nil {
		t.Errorf("Search with corrupt created_at succeeded, want error")
	}
}

func TestEntryTitle(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"first line", "docker fix\ndetails here", "docker fix"},
		{"single line", "one liner", "one liner"},
		{"trims", "  padded title  \nrest", "padded title"},
		{"truncates long", strings.Repeat("x", 100), strings.Repeat("x", 80) + "…"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := (Entry{Body: tt.body}).Title(); got != tt.want {
				t.Errorf("Title() = %q, want %q", got, tt.want)
			}
		})
	}
}

func ids(entries []Entry) []int64 {
	out := make([]int64, len(entries))
	for i, e := range entries {
		out[i] = e.ID
	}
	return out
}
