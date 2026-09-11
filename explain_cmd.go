package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
)

// tilegen explain [SPEC] [STORE...] shows, for every store, its needs, the
// legal backends with their scores, the illegal ones with the reason, and
// which one won and why.
func explainCmd(args []string, stdout, log io.Writer) error {
	fs := flag.NewFlagSet("tilegen explain", flag.ExitOnError)
	var o Options
	fs.StringVar(&o.Config, "config", "", "config .sexp file (default: a (config ...) form in the spec)")
	fs.StringVar(&o.Out, "out", "", "the generated project, whose tilegen.lock pins choices (default: the workspace's (out ...), else ./out)")
	fs.BoolVar(&o.Reselect, "reselect", false, "ignore tilegen.lock: show what a fresh choice would be")
	fs.StringVar(&o.PolicyFile, "policy", "", "policy .sexp file (default: a (policy ...) form in the spec)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: tilegen explain [flags] [SPEC] [STORE...]\n\n")
		fs.PrintDefaults()
	}
	pos := parseAnywhere(fs, args)
	o.Spec = "spec"
	if len(pos) > 0 {
		if _, err := os.Stat(pos[0]); err == nil {
			o.Spec, pos = pos[0], pos[1:]
		}
	}
	cp, err := compile(o, log)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(cp.c.Choices))
	for n := range cp.c.Choices {
		names = append(names, n)
	}
	sort.Strings(names)
	fmt.Fprintf(stdout, "%s\n\n", cp.c.Policy.describe())
	shown := 0
	for _, n := range names {
		if len(pos) > 0 && !matchesAny(n, pos) {
			continue
		}
		fmt.Fprintln(stdout, explain(cp.c.Choices[n]))
		shown++
	}
	if shown == 0 {
		if len(pos) > 0 {
			return fmt.Errorf("no store %s%s", pos[0], didYouMean(pos[0], names))
		}
		fmt.Fprintln(stdout, "no stores in this spec")
	}
	return nil
}
