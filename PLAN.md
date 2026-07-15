# Plan: `til` — local-only personal engineering knowledge base
_Locked via grill — by Claude + Ray Cho. Revised after Codex review round 1._

## Goal

A single-binary CLI tool (Go + HTMX + SQLite) for capturing and retrieving personal engineering notes. Capture is CLI-first (`til add`), retrieval is a read-only localhost web UI with FTS5 live search plus terminal search. One SQLite file at `~/.til/til.db` holds everything; there is no deployment, no network exposure, no external services. The binary cross-compiles to macOS/Linux/Windows with `CGO_ENABLED=0` so it can be shared with colleagues as a single file. Published to `github.com/valkyraycho/til` (MIT).

## Approach

1. **Scaffold**: create repo at `~/go-htmx-sqlite/til`, `git init`, `go mod init github.com/valkyraycho/til`, MIT license. Layout:
   ```
   main.go                  # subcommand dispatcher (stdlib flag, no cobra)
   internal/store/          # SQLite open/migrate/CRUD/search
   internal/web/            # HTTP server, handlers, embedded templates
   internal/editor/         # $EDITOR round-trip
   internal/render/         # goldmark markdown wrapper
   ```
2. **Store layer (TDD, hardest-tested)**. Driver: `modernc.org/sqlite` (pure Go). Schema:
   ```sql
   CREATE TABLE entries (
     id         INTEGER PRIMARY KEY AUTOINCREMENT,  -- never-reused ids; stale URLs can't show wrong notes
     body       TEXT NOT NULL,
     created_at TEXT NOT NULL                       -- fixed-width RFC3339 UTC, second precision ("2006-01-02T15:04:05Z")
   );
   CREATE TABLE entry_tags (
     entry_id INTEGER NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
     tag      TEXT NOT NULL,
     PRIMARY KEY (entry_id, tag)
   );
   CREATE INDEX idx_entry_tags_tag ON entry_tags(tag, entry_id);  -- tag-leading lookups
   CREATE VIRTUAL TABLE entries_fts USING fts5(
     body, tags,
     tokenize = 'porter unicode61'
   );
   ```
   - **Connection config via DSN pragmas** (applies to every pooled connection, not just the first): `_pragma=foreign_keys(1)`, `_pragma=busy_timeout(5000)`, `_pragma=journal_mode(WAL)`. `database/sql` pooling means per-connection PRAGMAs must live in the DSN.
   - **Recency ordering is `ORDER BY id DESC`** (monotonic via AUTOINCREMENT); `created_at` is display-only, so timestamp precision can never affect sort order.
   - **Migrations**: `PRAGMA user_version` versioning, executed inside `BEGIN IMMEDIATE` with the version re-read after acquiring the write lock (two processes racing first-run cannot double-apply DDL). A `user_version` newer than the binary knows → hard error ("db created by newer til").
   - **FTS sync in the same Go transaction** as every insert/update/delete (no SQL triggers); tags space-joined into the `tags` column. **Every FTS write addresses `rowid` explicitly**: `INSERT INTO entries_fts(rowid, body, tags) VALUES (:entry_id, ...)`, updates/deletes by that same rowid — the mapping is never left to FTS5's own rowid assignment (which could drift from `entries.id` after delete-highest → add).
   - **FTS query builder** (no raw user input in MATCH): tokenize on whitespace, wrap every token in double quotes with internal `"` doubled, append `*` to the final token for prefix search. Query length capped (256 chars). Results `ORDER BY bm25(entries_fts), id DESC`, paged via `LIMIT ? OFFSET ?` (page size 50).
   - **Search semantics fully specified**: blank query + no tag → recent (`id DESC`); blank query + tag → tag-filtered, `id DESC`; query + tag → FTS match intersected with tag join. All three paths paged identically.
   - **File permissions**: `~/.til` created `0700`, database file `0600`.
   - **Tags normalized** at the boundary: trim, lowercase, reject empty, **dedupe after normalization** (`-t Go,go` → one `go`); inserts use `INSERT OR IGNORE` so duplicates can never violate the PK.
3. **CLI commands** in `main.go` + thin command funcs:
   - `til add [-t tag1,tag2] ["text"]` — input precedence: inline arg → piped stdin → editor.
   - `til list [-n 20]` — newest first (`id DESC`); first line of body shown as title.
   - `til search [-n 20] <query>` — FTS5 match via the query builder, prints id/date/title snippet; `-n` caps results like `list`.
   - `til edit <id>` — body → temp file (0600) → editor → save back (tags unchanged).
   - `til rm <id>` — delete (FTS row removed in same tx).
   - `til web [--port 4711]` — serve UI on `127.0.0.1` only.
   - **Editor resolution**: `$VISUAL` → `$EDITOR` → platform default (`vi` on unix, `notepad` on Windows). Values parsed with a small quote-aware splitter (double quotes group; `"C:\Program Files\...\code.exe" -w` and `code -w` both work), tested with paths containing spaces. Non-zero editor exit → abort, no write. Empty/whitespace-only content → abort with "empty note, not saved".
   - DB path: `~/.til/til.db` (via `os.UserHomeDir()`), overridable with `TIL_DB` env var. Directory auto-created on first use.
4. **Web UI** (read-only, single page, HTMX):
   - `GET /` — search box + recent entries + tag chips.
   - `GET /search?q=...&tag=...&offset=...` — HTML fragment of result rows (page of 50); `hx-trigger="keyup changed delay:300ms"`; prefix matching via the query builder. A trailing "load more" row fetches the next page (`hx-get` with bumped offset) — older matches stay reachable.
   - `GET /entries/{id}` — full entry fragment, markdown rendered.
   - Templates + htmx.min.js embedded via `go:embed` (no CDN; works offline).
   - Markdown via `goldmark`, raw HTML passthrough disabled (never `WithUnsafe`); template output via `html/template` escaping; goldmark output is the only pre-escaped HTML injected.
   - **Server hardening (cheap, standard)**:
     - Host-header validation: requests whose `Host` isn't `127.0.0.1:<port>` or `localhost:<port>` get 403 — kills DNS-rebinding.
     - Response headers: `Content-Security-Policy: default-src 'self'`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, `Cache-Control: no-store`. CSP neutralizes `javascript:`/inline-script vectors that markdown links could smuggle.
     - `net.Listen` first, print URL only after the listener is live; `ReadHeaderTimeout`/`ReadTimeout`/`WriteTimeout` set; graceful shutdown on SIGINT/SIGTERM; handler errors logged to stderr.
5. **Tests** (TDD throughout, table-driven, target ≥80%):
   - store: real SQLite files in `t.TempDir()` — CRUD, FTS stemming ("deploying" matches deploy), prefix match, tag filter + normalization + dedupe, rm syncs FTS, migration idempotency + newer-version rejection, **concurrent first-run migration** (two stores racing a fresh DB path), **FTS query builder hostile inputs** (`"`, `AND`, `-`, `(`, blank, 10KB string), **concurrent writers** (two store instances, busy_timeout honored), **FK cascade across pooled-connection replacement** (`SetMaxIdleConns(0)` forces fresh connections mid-test), rowid non-reuse after delete, **FTS↔entry mapping after delete-highest → add → search**, unix file permissions.
   - web: `httptest` — search fragment, detail render, `<script>` in body inert, **hostile markdown vectors** (`javascript:` link, raw HTML block, `<img onerror>`), security headers present, Host-validation 403.
   - render: golden cases (code block, link, raw HTML stripped).
   - editor/main: smoke tests (editor-with-args parsing, nonzero exit aborts).
6. **CI**: GitHub Actions matrix (ubuntu/macos/windows) — `CGO_ENABLED=0 go build` + `go test ./...`. Backs the cross-platform promise with actual runs.
7. **Publish** (requires one-time user action — no credential on this machine reaches `valkyraycho`): user runs `gh auth login` (browser) for valkyraycho; then `gh auth switch --user valkyraycho`, `gh repo create valkyraycho/til --public --source . --push`, switch back to EMU account. HTTPS via gh credential helper; no SSH setup.

## Key decisions & tradeoffs

- **No title field** — first line of body doubles as display title.
- **Tags first-class** (`-t` flag, separate table), normalized lowercase; topic browse ≠ word match.
- **`modernc.org/sqlite` over `mattn/go-sqlite3`** — pure Go buys `CGO_ENABLED=0` cross-compilation; ~1.5–2× slower, irrelevant at this scale.
- **FTS5 keyword search only** — no vector/semantic search; contradicts local-only + simple.
- **Web UI read-only; all mutations via CLI** — deletes the form/validation/CSRF surface entirely.
- **No auth token on the localhost UI** — Host validation + CSP + loopback bind are the defenses. Accepted residual risk, documented in README: on a *shared multi-user machine*, other local OS users can read notes via the port while `til web` runs. For a personal-laptop tool the token's UX cost (unshareable URLs, copy-paste ritual every launch) outweighs it. Revisit only if someone actually runs this on a shared host.
- **`ORDER BY id DESC` for recency + AUTOINCREMENT** — sidesteps timestamp-precision sorting entirely; `created_at` becomes display-only.
- **FTS sync in Go transactions, not SQL triggers** — one testable write path; store is the single writer.
- **stdlib CLI dispatcher, no cobra**; **dependencies capped at two** (sqlite driver + goldmark); HTMX embedded as a static file.
- **Bind 127.0.0.1 only, default port 4711**.
- **WAL + busy_timeout(5000)** — `til web` and a concurrent `til add` coexist; writers retry up to 5s instead of failing with `SQLITE_BUSY`.

## Risks / open questions

- **Publishing needs interactive auth** — all machine credentials map to work accounts; push to valkyraycho requires one-time `gh auth login`. Build/commit proceed regardless.
- **modernc FTS5 + porter tokenizer** — exercised by store tests on day one; fallback is mattn/go-sqlite3 at the cost of cgo.
- **Windows** — designed-for (editor fallback, `os.UserHomeDir()`), validated by CI matrix rather than manual testing.

## Out of scope

- Vector/semantic search, embeddings, RAG.
- Sync/backup tooling. README documents the safe manual story: copy the DB only when no `til` process is running **and no `til.db-wal`/`til.db-shm` sidecar files exist** (a clean close checkpoints and removes them; after a crash they may linger — run any til command, e.g. `til list`, and let it exit to recover/checkpoint first). Live-syncing the WAL file trio is explicitly unsupported.
- Web-side editing, auth, multi-user, HTTPS, deployment of any kind.
- Import/export commands, shell completions, config files.
