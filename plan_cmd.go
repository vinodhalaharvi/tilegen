package main

import (
	"flag"
	"fmt"
	"github.com/vinodhalaharvi/tilegen/internal/compile"
	"io"
	"os"
	"strings"
)

// tilegen plan [-dot] [SPEC] prints the plan graph.
func planCmd(args []string, stdout, log io.Writer) error {
	fs := flag.NewFlagSet("tilegen plan", flag.ExitOnError)
	var o compile.Options
	fs.StringVar(&o.Config, "config", "", "config .sexp file (default: a (config ...) form in the spec)")
	fs.StringVar(&o.Out, "out", "", "the generated project (default: the workspace's (out ...), else ./out)")
	fs.StringVar(&o.PolicyFile, "policy", "", "policy .sexp file")
	dot := fs.Bool("dot", false, "print graphviz DOT instead of levels")
	asJSON := fs.Bool("json", false, "print the levels as JSON")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: tilegen plan [-dot] [SPEC]\n\nWhat the plan makes, in dependency order. Writes nothing.\n\n")
		fs.PrintDefaults()
	}
	pos := compile.ParseAnywhere(fs, args)
	o.Spec = "spec"
	if len(pos) > 0 {
		o.Spec = pos[0]
	}
	cp, err := compile.Compile(o, log)
	if err != nil {
		return err
	}
	r, err := compile.Emit(cp.Nodes, cp.Opts.Out, cp.Ctx, false) // plan only: nothing is written
	if err != nil {
		return err
	}
	g := compile.BuildPlanGraph(r, cp.Ctx)
	if *dot {
		fmt.Fprint(stdout, g.Dot())
		return nil
	}
	levels, cycleErr := g.Levels()
	if *asJSON {
		if err := compile.WriteJSON(stdout, compile.NewPlanJSON(g, levels, cycleErr)); err != nil {
			return err
		}
		return cycleErr
	}
	err = cycleErr
	for i, level := range levels {
		fmt.Fprintf(stdout, "level %d\n", i)
		for _, id := range level {
			n := g.Nodes[id]
			line := fmt.Sprintf("  %-6s %s", n.Kind, n.Name)
			switch {
			case n.By != "" && n.Note != "":
				line += fmt.Sprintf("   (%s: %s)", n.By, n.Note)
			case n.By != "":
				line += fmt.Sprintf("   (%s)", n.By)
			case n.Note != "":
				line += "   " + shortNote(n.Note)
			}
			fmt.Fprintln(stdout, line)
		}
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "\n%d node(s) in %d level(s); everything in a level is independent of the rest of it.\n", len(g.Order), len(levels))
	return nil
}

func shortNote(s string) string {
	if len(s) <= 70 {
		return s
	}
	if i := strings.IndexByte(s[40:], '.'); i > 0 && 40+i < 70 {
		return s[:40+i+1]
	}
	return s[:67] + "..."
}
