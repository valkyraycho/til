package web

import (
	"bytes"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/valkyraycho/til/internal/render"
	"github.com/valkyraycho/til/internal/store"
)

type rowsData struct {
	Entries []store.Entry
	NextURL string
}

type composerData struct {
	OOB bool
}

type indexData struct {
	Rows     rowsData
	Tags     []string
	Today    string
	Composer composerData
}

type entryData struct {
	Entry store.Entry
	HTML  any
}

type editData struct {
	Entry store.Entry
	Tags  string
}

func (s *server) index(w http.ResponseWriter, r *http.Request) {
	entries, err := s.store.Search("", "", pageSize, 0)
	if err != nil {
		s.fail(w, err)
		return
	}
	tags, err := s.store.Tags()
	if err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, "index.html", indexData{
		Rows:  rowsData{Entries: entries, NextURL: nextURL("", "", 0, len(entries))},
		Tags:  tags,
		Today: strings.ToLower(time.Now().Format("Monday · 02 January 2006")),
	})
}

func (s *server) search(w http.ResponseWriter, r *http.Request) {
	q := r.FormValue("q")
	tag := r.FormValue("tag")
	offset, err := strconv.Atoi(r.FormValue("offset"))
	if err != nil || offset < 0 {
		offset = 0
	}
	entries, err := s.store.Search(q, tag, pageSize, offset)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, "rows.html", rowsData{Entries: entries, NextURL: nextURL(q, tag, offset, len(entries))})
}

func (s *server) entry(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r)
	if !ok {
		return
	}
	s.renderEntry(w, id)
}

func (s *server) entryRow(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r)
	if !ok {
		return
	}
	e, ok := s.getEntry(w, id)
	if !ok {
		return
	}
	s.render(w, "row.html", e)
}

func (s *server) entryEditForm(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r)
	if !ok {
		return
	}
	e, ok := s.getEntry(w, id)
	if !ok {
		return
	}
	s.render(w, "edit.html", editData{Entry: e, Tags: strings.Join(e.Tags, ", ")})
}

func (s *server) entryCreate(w http.ResponseWriter, r *http.Request) {
	body := r.FormValue("body")
	if strings.TrimSpace(body) == "" {
		http.Error(w, "note body is empty", http.StatusBadRequest)
		return
	}
	if _, err := s.store.Add(body, strings.Split(r.FormValue("tags"), ",")); err != nil {
		s.fail(w, err)
		return
	}
	entries, err := s.store.Search("", "", pageSize, 0)
	if err != nil {
		s.fail(w, err)
		return
	}
	var buf bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&buf, "rows.html",
		rowsData{Entries: entries, NextURL: nextURL("", "", 0, len(entries))}); err != nil {
		s.fail(w, err)
		return
	}
	if err := s.tmpl.ExecuteTemplate(&buf, "composer.html", composerData{OOB: true}); err != nil {
		s.fail(w, err)
		return
	}
	if _, err := buf.WriteTo(w); err != nil {
		log.Printf("til web: write response: %v", err)
	}
}

func (s *server) entryUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r)
	if !ok {
		return
	}
	body := r.FormValue("body")
	if strings.TrimSpace(body) == "" {
		http.Error(w, "note body is empty", http.StatusBadRequest)
		return
	}
	err := s.store.Update(id, body, strings.Split(r.FormValue("tags"), ","))
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "entry not found", http.StatusNotFound)
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	s.renderEntry(w, id)
}

func (s *server) entryDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r)
	if !ok {
		return
	}
	err := s.store.Delete(id)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "entry not found", http.StatusNotFound)
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *server) renderEntry(w http.ResponseWriter, id int64) {
	e, ok := s.getEntry(w, id)
	if !ok {
		return
	}
	html, err := render.Markdown(e.Body)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, "entry.html", entryData{Entry: e, HTML: html})
}

func (s *server) pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid entry id", http.StatusBadRequest)
		return 0, false
	}
	return id, true
}

func (s *server) getEntry(w http.ResponseWriter, id int64) (store.Entry, bool) {
	e, err := s.store.Get(id)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "entry not found", http.StatusNotFound)
		return store.Entry{}, false
	}
	if err != nil {
		s.fail(w, err)
		return store.Entry{}, false
	}
	return e, true
}

func nextURL(q, tag string, offset, got int) string {
	if got < pageSize {
		return ""
	}
	v := url.Values{}
	if q != "" {
		v.Set("q", q)
	}
	if tag != "" {
		v.Set("tag", tag)
	}
	v.Set("offset", strconv.Itoa(offset+pageSize))
	return "/search?" + v.Encode()
}

func (s *server) render(w http.ResponseWriter, name string, data any) {
	var buf bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		s.fail(w, err)
		return
	}
	if _, err := buf.WriteTo(w); err != nil {
		log.Printf("til web: write response: %v", err)
	}
}

func (s *server) fail(w http.ResponseWriter, err error) {
	log.Printf("til web: %v", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}
