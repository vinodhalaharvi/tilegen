package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"

	"golang.org/x/term"
)

// tilegen tiles [-sexp] [FILTER]
//
// lists the registry: every tile, the pass it runs in, the capability it
// produces, its declared cost, and the tools it needs (and whether they are
// installed). FILTER keeps tiles whose name, pass or capability contains it.
func tilesCmd(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("tilegen tiles", flag.ExitOnError)
	asSexp := fs.Bool("sexp", false, "print the registry as S-expressions")
	verbose := fs.Bool("v", false, "do not trim the table to the terminal width")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: tilegen tiles [-sexp] [-v] [FILTER]\n\n")
		fs.PrintDefaults()
	}
	pos := parseAnywhere(fs, args)
	var rows []TileInfo
	for _, t := range Registry() {
		if len(pos) == 0 || strings.Contains(t.Name+" "+t.Pass+" "+t.Produces, pos[0]) {
			rows = append(rows, t)
		}
	}
	if *asSexp {
		for _, t := range rows {
			fmt.Fprintln(stdout, tileSexp(t).String())
		}
		return nil
	}
	var buf strings.Builder
	tw := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "PASS\tTILE\tPRODUCES\tCOST\tNEEDS\tWHAT IT DOES")
	for _, t := range rows {
		var needs []string
		for _, r := range t.Requires {
			state := "missing"
			if _, err := exec.LookPath(r); err == nil {
				state = "found"
			}
			needs = append(needs, r+" ("+state+")")
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", t.Pass, t.Name, t.Produces, t.Cost.Short(), strings.Join(needs, ", "), t.Doc)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	width := 0 // trim rows to the terminal, so narrow tmux panes stay readable
	if f, ok := stdout.(*os.File); ok && !*verbose && term.IsTerminal(int(f.Fd())) {
		width, _, _ = term.GetSize(int(f.Fd()))
	}
	for _, line := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		if r := []rune(line); width > 0 && len(r) > width {
			line = string(r[:width-1]) + "…"
		}
		fmt.Fprintln(stdout, strings.TrimRight(line, " "))
	}
	fmt.Fprintf(stdout, "\n%d tile(s). Storage backends: %s. Costs are declared; selection by cost is next on the roadmap.\n",
		len(rows), strings.Join(backendNames(), ", "))
	return nil
}
