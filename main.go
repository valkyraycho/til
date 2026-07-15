package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/valkyraycho/til/internal/store"
)

const usage = `til — personal engineering knowledge base

usage:
  til add [-t tag1,tag2] ["note text"]   capture a note (arg, piped stdin, or $EDITOR)
  til list [-n 20]                       recent notes
  til search [-n 20] <query>             full-text search
  til edit <id>                          reopen a note in $EDITOR
  til rm <id>                            delete a note
  til web [-port 4711]                   browse and search at http://127.0.0.1:4711

database: ~/.til/til.db (override with TIL_DB)
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch cmd := os.Args[1]; cmd {
	case "add":
		err = cmdAdd(os.Args[2:])
	case "list":
		err = cmdList(os.Args[2:])
	case "search":
		err = cmdSearch(os.Args[2:])
	case "edit":
		err = cmdEdit(os.Args[2:])
	case "rm":
		err = cmdRm(os.Args[2:])
	case "web":
		err = cmdWeb(os.Args[2:])
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "til: unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "til:", err)
		os.Exit(1)
	}
}

func dbPath() (string, error) {
	if p := os.Getenv("TIL_DB"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory: %w", err)
	}
	return filepath.Join(home, ".til", "til.db"), nil
}

func openStore() (*store.Store, error) {
	path, err := dbPath()
	if err != nil {
		return nil, err
	}
	return store.Open(path)
}
