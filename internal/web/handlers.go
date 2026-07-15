package web

import (
	"bytes"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strconv"

	"github.com/valkyraycho/til/internal/render"
	"github.com/valkyraycho/til/internal/store"
)

type rowsData struct {
	Entries []store.Entry
	NextURL string
}

type indexData struct {
	Rows rowsData
	Tags []string
}

type entryData struct {
	Entry store.Entry
	HTML  any
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
		Rows: rowsData{Entries: entries, NextURL: nextURL("", "", 0, len(entries))},
		Tags: tags,
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
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid entry id", http.StatusBadRequest)
		return
	}
	e, err := s.store.Get(id)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "entry not found", http.StatusNotFound)
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	html, err := render.Markdown(e.Body)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, "entry.html", entryData{Entry: e, HTML: html})
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
