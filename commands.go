package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/valkyraycho/til/internal/editor"
	"github.com/valkyraycho/til/internal/store"
	"github.com/valkyraycho/til/internal/web"
)

const (
	defaultListLimit = 20
	defaultPort      = 4711
)

func cmdAdd(args []string) error {
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	tags := fs.String("t", "", "comma-separated tags")
	if err := fs.Parse(args); err != nil {
		return err
	}
	body, err := noteInput(strings.Join(fs.Args(), " "))
	if err != nil {
		return err
	}
	if strings.TrimSpace(body) == "" {
		return errors.New("empty note, not saved")
	}
	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()

	e, err := st.Add(body, strings.Split(*tags, ","))
	if err != nil {
		return err
	}
	fmt.Printf("added #%d  %s\n", e.ID, e.Title())
	return nil
}

func noteInput(arg string) (string, error) {
	if strings.TrimSpace(arg) != "" {
		return arg, nil
	}
	stat, err := os.Stdin.Stat()
	if err == nil && stat.Mode()&os.ModeCharDevice == 0 {
		piped, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		return string(piped), nil
	}
	return editor.Edit("")
}

func cmdList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	limit := fs.Int("n", defaultListLimit, "max notes to show")
	if err := fs.Parse(args); err != nil {
		return err
	}
	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()

	entries, err := st.Search("", "", *limit, 0)
	if err != nil {
		return err
	}
	printEntries(entries)
	return nil
}

func cmdSearch(args []string) error {
	fs := flag.NewFlagSet("search", flag.ExitOnError)
	limit := fs.Int("n", defaultListLimit, "max results to show")
	if err := fs.Parse(args); err != nil {
		return err
	}
	query := strings.Join(fs.Args(), " ")
	if strings.TrimSpace(query) == "" {
		return errors.New("search needs a query (see: til search -h)")
	}
	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()

	entries, err := st.Search(query, "", *limit, 0)
	if err != nil {
		return err
	}
	printEntries(entries)
	return nil
}

func cmdEdit(args []string) error {
	id, err := parseID(args)
	if err != nil {
		return err
	}
	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()

	e, err := st.Get(id)
	if err != nil {
		return err
	}
	body, err := editor.Edit(e.Body)
	if err != nil {
		return err
	}
	if strings.TrimSpace(body) == "" {
		return errors.New("empty note, not saved")
	}
	if err := st.UpdateBody(id, body); err != nil {
		return err
	}
	fmt.Printf("updated #%d\n", id)
	return nil
}

func cmdRm(args []string) error {
	id, err := parseID(args)
	if err != nil {
		return err
	}
	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()

	if err := st.Delete(id); err != nil {
		return err
	}
	fmt.Printf("deleted #%d\n", id)
	return nil
}

func cmdWeb(args []string) error {
	fs := flag.NewFlagSet("web", flag.ExitOnError)
	port := fs.Int("port", defaultPort, "port to serve on (127.0.0.1 only)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()

	return web.Serve(st, *port)
}

func parseID(args []string) (int64, error) {
	if len(args) != 1 {
		return 0, errors.New("expected exactly one note id")
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid note id %q", args[0])
	}
	return id, nil
}

func printEntries(entries []store.Entry) {
	if len(entries) == 0 {
		fmt.Println("no notes found")
		return
	}
	for _, e := range entries {
		line := fmt.Sprintf("#%-5d %s  %s", e.ID, e.CreatedAt.Format("2006-01-02"), e.Title())
		if len(e.Tags) > 0 {
			line += "  [" + strings.Join(e.Tags, " ") + "]"
		}
		fmt.Println(line)
	}
}
