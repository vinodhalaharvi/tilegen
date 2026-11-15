package main

import (
	"flag"
	"fmt"
	"github.com/vinodhalaharvi/tilegen/internal/compile"
	"github.com/vinodhalaharvi/tilegen/internal/fill"
	"io"
	"os"
	"sort"
)

// tilegen explain [SPEC] [STORE...] shows, for every store, its needs, the
// legal backends with their scores, the illegal ones with the reason, and
// which one won and why.
func explainCmd(args []string, stdout, log io.Writer) error {
	fs := flag.NewFlagSet("tilegen explain", flag.ExitOnError)
	var o compile.Options
	fs.StringVar(&o.Config, "config", "", "config .sexp file (default: a (config ...) form in the spec)")
	fs.StringVar(&o.Out, "out", "", "the generated project, whose tilegen.lock pins choices (default: the workspace's (out ...), else ./out)")
	fs.BoolVar(&o.Reselect, "reselect", false, "ignore tilegen.lock: show what a fresh choice would be")
	asJSON := fs.Bool("json", false, "print the coverings as JSON")
	whole := fs.Bool("project", false, "also compare the need-by-need choice with the cheapest for the project as a whole")
	fs.StringVar(&o.PolicyFile, "policy", "", "policy .sexp file (default: a (policy ...) form in the spec)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: tilegen explain [flags] [SPEC] [STORE...]\n\n")
		fs.PrintDefaults()
	}
	pos := compile.ParseAnywhere(fs, args)
	o.Spec = "spec"
	if len(pos) > 0 {
		if _, err := os.Stat(pos[0]); err == nil {
			o.Spec, pos = pos[0], pos[1:]
		}
	}
	cp, err := compile.Compile(o, log)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(cp.Ctx.Cover))
	for n := range cp.Ctx.Cover {
		names = append(names, n)
	}
	sort.Strings(names)
	if *whole && (*asJSON || len(pos) > 0) {
		return fmt.Errorf("-project reports on the whole spec, as text: drop -json and the need names")
	}
	if *asJSON {
		compile.JSONWeights = cp.Ctx.Policy.Weights
		out := compile.ExplainJSON{Policy: compile.NewPolicyJSON(cp.Ctx.Policy)}
		for _, n := range names {
			if len(pos) == 0 || fill.MatchesAny(n, pos) {
				out.Coverage = append(out.Coverage, compile.NewCoverageJSON(cp.Ctx.Cover[n]))
			}
		}
		if len(out.Coverage) == 0 && len(pos) > 0 {
			return fmt.Errorf("no need %s%s", pos[0], compile.DidYouMean(pos[0], names))
		}
		return compile.WriteJSON(stdout, out)
	}
	fmt.Fprintf(stdout, "%s\n\n", cp.Ctx.Policy.Describe())
	shown := 0
	for _, n := range names {
		if len(pos) > 0 && !fill.MatchesAny(n, pos) {
			continue
		}
		fmt.Fprintln(stdout, compile.ExplainCovering(cp.Ctx.Cover[n]))
		shown++
	}
	if shown == 0 {
		if len(pos) > 0 {
			return fmt.Errorf("no store %s%s", pos[0], compile.DidYouMean(pos[0], names))
		}
		fmt.Fprintln(stdout, "no stores in this spec")
	}
	if *whole {
		compile.NewProjectView(cp.Ctx).Print(stdout)
	}
	return nil
}
