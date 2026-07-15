# til

A local-only personal engineering knowledge base. Capture notes from the
terminal, find them again with full-text search — in the terminal or in a
tiny localhost web UI.

One static binary (Go), one SQLite file, zero deployment. Built on
Go + HTMX + SQLite.

## Install

```sh
go install github.com/valkyraycho/til@latest
```

Or build a binary for any platform (pure Go, no C toolchain needed):

```sh
CGO_ENABLED=0 go build -o til            # current platform
GOOS=windows GOARCH=amd64 go build -o til.exe
GOOS=linux   GOARCH=amd64 go build -o til
```

## Usage

```sh
til add "traefik strips the trailing slash on StripPrefix"   # quick capture
til add -t docker,dns "containers can't resolve DNS? check /etc/resolv.conf"
kubectl describe pod broken | til add -t k8s                 # pipe anything in
til add                                                      # opens $EDITOR for longer notes

til list                       # recent notes
til search compose network     # full-text search (stemmed + prefix matching)
til edit 12                    # reopen a note in $EDITOR
til rm 12                      # delete a note

til web                        # browse + live search at http://127.0.0.1:4711
```

The web UI does everything the CLI does except open your `$EDITOR`: live
search as you type, tag chips, markdown rendering, plus creating, editing,
and deleting notes inline. Press `/` anywhere to jump to search.

## Data

Everything lives in a single SQLite file at `~/.til/til.db`. Point `TIL_DB`
at another path to relocate it:

```sh
export TIL_DB=~/Documents/notes/til.db
```

**Backup:** copy `til.db` whenever no `til` process is running *and* no
`til.db-wal`/`til.db-shm` files sit next to it (a clean close removes them;
after a crash, run any command like `til list` and let it exit first).
Live-syncing the database while `til web` is running is unsupported and can
corrupt the copy.

## Security notes

`til web` binds to `127.0.0.1` only, validates the `Host` header (blocks
DNS rebinding), rejects cross-site form POSTs via `Origin` checking (CSRF),
and serves with a strict `Content-Security-Policy`. On a *shared multi-user
machine*, be aware that other local users can reach the port while `til web`
runs — this tool assumes a personal machine.

## Development

```sh
go test ./...
```

## License

MIT
