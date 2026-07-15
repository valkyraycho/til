# Plan Review Log: til — local-only personal engineering knowledge base

Act 1 (grill) complete — plan locked with the user. MAX_ROUNDS=5.

## Round 1 — Codex (gpt-5.6-sol, session 019f6356-97d2-7210-8d7c-8243363efe9b)

Material problems remain in PLAN.md:

1. **Loopback is not private:** Other local users can read the HTTP service, and missing Host validation permits DNS-rebinding attacks. **Fix:** Require a random per-run token, validate `Host` as `127.0.0.1:<port>`, and use a strict SameSite cookie.
2. **Database permissions are unspecified:** Default umasks may make personal notes readable by other users. **Fix:** Create the directory as `0700`, database as `0600`, reject symlinks/non-regular files, and verify WAL sidecar permissions.
3. **Per-connection PRAGMAs are underspecified:** Running `PRAGMA foreign_keys=ON` once on `sql.DB` does not configure later pooled connections. **Fix:** Apply foreign keys and busy timeout through modernc's per-connection DSN/hook and test cascade behavior after connection replacement.
4. **WAL does not eliminate write contention:** Concurrent writers can still fail immediately with `SQLITE_BUSY`. **Fix:** Configure a bounded busy timeout/retry policy and test concurrent writes using separate store instances.
5. **First-run migration races:** Two processes can both observe `user_version=0` and attempt the same DDL. **Fix:** Re-read and update the version inside a `BEGIN IMMEDIATE` migration transaction, rejecting databases newer than the binary.
6. **FTS query construction is undefined:** Quotes, operators, punctuation, or blank input can produce FTS5 syntax errors; blindly adding `*` is unsafe. **Fix:** Build a bound literal-prefix expression with correct FTS quoting, reject oversized queries, and branch blank queries to recent/tag-only search.
7. **Live search is unbounded and nondeterministic:** Common prefixes can return/render the entire database, while no ranking or tie-break order is specified. **Fix:** Define `bm25` plus stable ID/date ordering and enforce a result limit with pagination or "more" behavior.
8. **RFC3339 text does not guarantee chronological sorting:** Variable fractional precision makes exact-second and fractional timestamps sort incorrectly. **Fix:** Store integer UTC nanoseconds or fixed-width timestamps, or order creation results by monotonic entry ID.
9. **Tag semantics conflict:** Binary tag uniqueness permits `Go` and `go`, empty tags, and control characters, while FTS applies different normalization; filtering also lacks a tag-leading index. **Fix:** Define normalization and validation, then add `CREATE INDEX entry_tags_tag_entry_id ON entry_tags(tag, entry_id)`.
10. **Entry IDs can be reused:** `INTEGER PRIMARY KEY` may reuse the highest deleted ID, causing stale URLs to display unrelated future notes. **Fix:** Use `INTEGER PRIMARY KEY AUTOINCREMENT` or another never-reused identifier.
11. **The Markdown security test is inadequate:** Marking generated output trusted bypasses template escaping, while raw-script testing misses dangerous URLs, SVG, remote images, and future renderer changes. **Fix:** Lock goldmark to safe options, test hostile Markdown vectors, and send restrictive CSP, `nosniff`, no-referrer, and no-store headers.
12. **The sync recommendation risks corruption/data loss:** WAL uses `til.db-wal` and `til.db-shm`, and file-sync tools cannot atomically synchronize the SQLite file set. **Fix:** Remove the synced-folder recommendation and require an offline/checkpointed copy or SQLite backup API.
13. **Editor behavior is incomplete:** `$EDITOR` commonly contains arguments, piped EOF can create an empty note, and failure/temp-file permissions are unspecified. **Fix:** Define and test editor command parsing, secure temp files, nonzero-exit cancellation, empty-input handling, and `VISUAL`/platform fallbacks.
14. **Server lifecycle lacks operational safeguards:** Printing the URL before binding can report a dead service, and default HTTP timeouts enable resource exhaustion without useful diagnostics. **Fix:** Bind first, validate the port, configure timeouts, propagate request cancellation, log handler failures, and shut down gracefully.
15. **The tests omit the plan's highest-risk promises:** Coverage percentage does not validate concurrent migration/write behavior, security headers, permissions, or actual target platforms. **Fix:** Add concurrency/security tests plus native Linux, macOS, and Windows CI builds/tests with `CGO_ENABLED=0`.

VERDICT: REVISE

### Claude's response (round 1)

**Accepted (13 of 15, some partially):**
- #1 partial — Host-header validation added (kills DNS rebinding, ~5 lines). **Rejected the per-run auth token + SameSite cookie**: threat is "other local OS users on a shared machine" — not the reality of this tool's personal-laptop audience; token makes the URL unshareable/un-bookmarkable every run. Residual risk documented in README + plan.
- #2 partial — dir 0700, db 0600 added. **Rejected symlink/non-regular-file checks**: `TIL_DB` is set by the same user who owns the data; there is no trust boundary between a user and their own env var.
- #3 accepted — pragmas moved into the DSN (`foreign_keys`, `busy_timeout`, `journal_mode`) so every pooled connection gets them. Real bug avoided.
- #4 accepted — `busy_timeout(5000)` + concurrent-writer test.
- #5 accepted — migrations in `BEGIN IMMEDIATE` with re-read; newer `user_version` → hard error.
- #6 accepted in full — explicit FTS query builder (quote every token, escape `"`, prefix-star last token, blank → recent, 256-char cap).
- #7 accepted — `LIMIT 50`, `ORDER BY bm25, id DESC`.
- #8 accepted via the simpler alternative — `ORDER BY id DESC` (AUTOINCREMENT-monotonic) for all recency; `created_at` demoted to display-only, stays fixed-width RFC3339 UTC. **Rejected nanosecond integer timestamps** as solving a problem the ordering change removes.
- #9 accepted — lowercase/trim/reject-empty normalization + tag-leading index.
- #10 accepted — `AUTOINCREMENT`.
- #11 accepted — CSP `default-src 'self'` + `nosniff` + `no-referrer` + `no-store` headers, goldmark locked safe, hostile-markdown test vectors. **Rejected adding an HTML sanitizer dependency** (bluemonday): CSP neutralizes `javascript:`/inline-script execution, content is self-authored, and the dependency cap (2) holds.
- #12 accepted as documentation — README states the DB is a clean single file only when no til process runs (WAL checkpoints on last clean close); live-syncing the WAL trio explicitly unsupported.
- #13 accepted — `$VISUAL`→`$EDITOR`→platform default, `strings.Fields` parsing, 0600 temp file, nonzero exit aborts, empty content aborts.
- #14 accepted — listen-before-print, server timeouts, graceful shutdown, stderr logging.
- #15 accepted — hostile-input/concurrency/header/permission tests added; GitHub Actions matrix (ubuntu/macos/windows, CGO_ENABLED=0).

**Net rejections (with reasons logged above):** per-run auth token, symlink checks, nanosecond timestamps, HTML sanitizer dependency.

## Round 2 — Codex (same session)

The rejected token, symlink, nanosecond timestamp, and sanitizer proposals are reasonably rejected for this threat model. Most other round-one findings are addressed.

Remaining issues:

1. **#7 is only partially fixed:** `LIMIT 50` without pagination makes older matches inaccessible through both search interfaces. **Fix:** Add UI pagination/load-more and a CLI `--limit`/`--all` option.
2. **#12 still has an unsafe claim:** After a crash or forced termination, no `til` process may be running while committed data remains in `-wal`. **Fix:** Permit copying only after a clean close with no sidecars; otherwise reopen and close `til` to recover/checkpoint first.
3. **#13 remains broken for quoted editor paths:** `strings.Fields` mishandles values such as `"C:\Program Files\...\code.exe" -w`. **Fix:** Use quote-aware, platform-appropriate command parsing and test executable paths containing spaces.
4. **Normalized duplicate tags can violate the primary key:** `-t Go,go` becomes two identical `go` inserts. **Fix:** Deduplicate tags after normalization and before insertion.
5. **Blank-query tag behavior is unspecified:** "Blank query → recent list" may bypass `tag`, breaking tag-chip browsing. **Fix:** Explicitly define blank query plus tag as tag-filtered results ordered by `id DESC`.
6. **The highest-risk connection races remain untested:** Concurrent writers do not exercise simultaneous first-run migration or replacement pooled connections. **Fix:** Add concurrent fresh-database migration and foreign-key cascade tests after forced connection replacement.

VERDICT: REVISE

### Claude's response (round 2)

All six accepted — each is a genuine spec gap with a cheap fix:
1. Paged search: `LIMIT ? OFFSET ?` (page 50) + HTMX "load more" row; CLI `search -n` flag mirroring `list`.
2. Backup wording tightened: copy only when no til process AND no `-wal`/`-shm` sidecars; after a crash, run any til command and exit to checkpoint first.
3. `strings.Fields` replaced with a small quote-aware splitter; test includes an executable path containing spaces.
4. Tags deduped after normalization + `INSERT OR IGNORE` (PK violation impossible).
5. Search semantics specified as a three-way matrix: blank+no-tag → recent; blank+tag → tag-filtered `id DESC`; query+tag → FTS ∩ tag.
6. Tests added: concurrent first-run migration on a fresh DB path; FK cascade with `SetMaxIdleConns(0)` forcing pooled-connection replacement.

## Round 3 — Codex (same session)

All six round-two findings are addressed. One new material schema omission remains:

- **FTS rowid mapping is no longer specified:** After deleting the newest entry, FTS5 may reuse its rowid while `entries.id AUTOINCREMENT` advances, causing joins to associate search results with the wrong note. **Fix:** Explicitly insert every FTS row with `rowid = entries.id`, update/delete by that rowid, and test delete-highest → add → search mapping.

VERDICT: REVISE

### Claude's response (round 3)

Accepted — the round-1 rewrite dropped the original "keyed by rowid = entries.id" contract. Plan now specifies every FTS write addresses rowid explicitly (`INSERT INTO entries_fts(rowid, body, tags) VALUES (:entry_id, ...)`) and adds the delete-highest → add → search mapping test.

## Round 4 — Codex (same session)

Confirmed: explicit FTS `rowid = entries.id` writes and the delete-highest regression test are present. The plan is sound enough to implement.

VERDICT: APPROVED

---
Converged in 4 rounds. Plan locked for implementation pending user sign-off.
