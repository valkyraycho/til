package store

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	maxQueryRunes = 256
	maxTitleRunes = 80
)

var ErrNotFound = errors.New("entry not found")

func (e Entry) Title() string {
	title, _, _ := strings.Cut(e.Body, "\n")
	title = strings.TrimSpace(title)
	if runes := []rune(title); len(runes) > maxTitleRunes {
		return string(runes[:maxTitleRunes]) + "…"
	}
	return title
}

func NormalizeTags(tags []string) []string {
	var out []string
	seen := make(map[string]bool, len(tags))
	for _, t := range tags {
		norm := strings.Join(strings.Fields(strings.ToLower(t)), "-")
		if norm == "" || seen[norm] {
			continue
		}
		seen[norm] = true
		out = append(out, norm)
	}
	return out
}

func (s *Store) Add(body string, tags []string) (Entry, error) {
	if strings.TrimSpace(body) == "" {
		return Entry{}, errors.New("note body is empty")
	}
	tags = NormalizeTags(tags)
	createdAt := time.Now().UTC().Truncate(time.Second)

	tx, err := s.db.Begin()
	if err != nil {
		return Entry{}, fmt.Errorf("begin add: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.Exec("INSERT INTO entries (body, created_at) VALUES (?, ?)",
		body, createdAt.Format(timeFormat))
	if err != nil {
		return Entry{}, fmt.Errorf("insert entry: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Entry{}, fmt.Errorf("read entry id: %w", err)
	}
	for _, tag := range tags {
		if _, err := tx.Exec("INSERT OR IGNORE INTO entry_tags (entry_id, tag) VALUES (?, ?)", id, tag); err != nil {
			return Entry{}, fmt.Errorf("insert tag %q: %w", tag, err)
		}
	}
	if _, err := tx.Exec("INSERT INTO entries_fts (rowid, body, tags) VALUES (?, ?, ?)",
		id, body, strings.Join(tags, " ")); err != nil {
		return Entry{}, fmt.Errorf("index entry: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Entry{}, fmt.Errorf("commit add: %w", err)
	}
	return Entry{ID: id, Body: body, Tags: tags, CreatedAt: createdAt}, nil
}

func (s *Store) Get(id int64) (Entry, error) {
	var e Entry
	var createdAt string
	err := s.db.QueryRow("SELECT id, body, created_at FROM entries WHERE id = ?", id).
		Scan(&e.ID, &e.Body, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Entry{}, ErrNotFound
	}
	if err != nil {
		return Entry{}, fmt.Errorf("get entry %d: %w", id, err)
	}
	e.CreatedAt, err = time.Parse(timeFormat, createdAt)
	if err != nil {
		return Entry{}, fmt.Errorf("parse created_at of entry %d: %w", id, err)
	}
	e.Tags, err = s.entryTags(id)
	if err != nil {
		return Entry{}, err
	}
	return e, nil
}

func (s *Store) Update(id int64, body string, tags []string) error {
	if strings.TrimSpace(body) == "" {
		return errors.New("note body is empty")
	}
	tags = NormalizeTags(tags)
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin update: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.Exec("UPDATE entries SET body = ? WHERE id = ?", body, id)
	if err != nil {
		return fmt.Errorf("update entry %d: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec("DELETE FROM entry_tags WHERE entry_id = ?", id); err != nil {
		return fmt.Errorf("clear tags of entry %d: %w", id, err)
	}
	for _, tag := range tags {
		if _, err := tx.Exec("INSERT OR IGNORE INTO entry_tags (entry_id, tag) VALUES (?, ?)", id, tag); err != nil {
			return fmt.Errorf("insert tag %q: %w", tag, err)
		}
	}
	if _, err := tx.Exec("UPDATE entries_fts SET body = ?, tags = ? WHERE rowid = ?",
		body, strings.Join(tags, " "), id); err != nil {
		return fmt.Errorf("reindex entry %d: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit update: %w", err)
	}
	return nil
}

func (s *Store) Delete(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin delete: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.Exec("DELETE FROM entries WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete entry %d: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec("DELETE FROM entries_fts WHERE rowid = ?", id); err != nil {
		return fmt.Errorf("deindex entry %d: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete: %w", err)
	}
	return nil
}

func (s *Store) Search(query, tag string, limit, offset int) ([]Entry, error) {
	query = strings.TrimSpace(query)
	if norm := NormalizeTags([]string{tag}); len(norm) == 1 {
		tag = norm[0]
	} else {
		tag = ""
	}

	var rows *sql.Rows
	var err error
	switch {
	case query == "" && tag == "":
		rows, err = s.db.Query(
			"SELECT id, body, created_at FROM entries ORDER BY id DESC LIMIT ? OFFSET ?",
			limit, offset)
	case query == "":
		rows, err = s.db.Query(
			`SELECT e.id, e.body, e.created_at FROM entries e
			 JOIN entry_tags t ON t.entry_id = e.id
			 WHERE t.tag = ? ORDER BY e.id DESC LIMIT ? OFFSET ?`,
			tag, limit, offset)
	case tag == "":
		rows, err = s.db.Query(
			`SELECT e.id, e.body, e.created_at FROM entries_fts
			 JOIN entries e ON e.id = entries_fts.rowid
			 WHERE entries_fts MATCH ?
			 ORDER BY bm25(entries_fts), e.id DESC LIMIT ? OFFSET ?`,
			ftsQuery(query), limit, offset)
	default:
		rows, err = s.db.Query(
			`SELECT e.id, e.body, e.created_at FROM entries_fts
			 JOIN entries e ON e.id = entries_fts.rowid
			 WHERE entries_fts MATCH ?
			   AND e.id IN (SELECT entry_id FROM entry_tags WHERE tag = ?)
			 ORDER BY bm25(entries_fts), e.id DESC LIMIT ? OFFSET ?`,
			ftsQuery(query), tag, limit, offset)
	}
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var e Entry
		var createdAt string
		if err := rows.Scan(&e.ID, &e.Body, &createdAt); err != nil {
			return nil, fmt.Errorf("scan search row: %w", err)
		}
		e.CreatedAt, err = time.Parse(timeFormat, createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse created_at of entry %d: %w", e.ID, err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search rows: %w", err)
	}
	// ponytail: N+1 tag loads; join with group_concat if page-size profiling ever demands it
	for i := range entries {
		entries[i].Tags, err = s.entryTags(entries[i].ID)
		if err != nil {
			return nil, err
		}
	}
	return entries, nil
}

func (s *Store) Tags() ([]string, error) {
	rows, err := s.db.Query("SELECT DISTINCT tag FROM entry_tags ORDER BY tag")
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, fmt.Errorf("scan tag: %w", err)
		}
		tags = append(tags, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tag rows: %w", err)
	}
	return tags, nil
}

func (s *Store) entryTags(id int64) ([]string, error) {
	rows, err := s.db.Query("SELECT tag FROM entry_tags WHERE entry_id = ? ORDER BY tag", id)
	if err != nil {
		return nil, fmt.Errorf("tags of entry %d: %w", id, err)
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, fmt.Errorf("scan tag of entry %d: %w", id, err)
		}
		tags = append(tags, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tag rows of entry %d: %w", id, err)
	}
	sort.Strings(tags)
	return tags, nil
}

func ftsQuery(query string) string {
	if runes := []rune(query); len(runes) > maxQueryRunes {
		query = string(runes[:maxQueryRunes])
	}
	fields := strings.Fields(query)
	parts := make([]string, len(fields))
	for i, f := range fields {
		parts[i] = `"` + strings.ReplaceAll(f, `"`, `""`) + `"`
	}
	if len(parts) > 0 {
		parts[len(parts)-1] += "*"
	}
	return strings.Join(parts, " ")
}
