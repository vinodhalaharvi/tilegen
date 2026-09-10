// Command tilegen compiles an S-expression spec into Go scaffolding by
// successive lowering passes and tree tiling, leaving well-described holes
// for an LLM (or a human) to fill.
//
//	spec.sexp --parse--> validate --expand--> --concretize(config)--> --select-->
//	target forms --emit--> go.mod, *.go, db/*.sql, sqlc.yaml, tilegen.tasks.json
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const version = "v0.1.0"

// Pipeline is the ordered list of lowering passes. Each one maps
// S-expressions to S-expressions; run with -dump to see every stage.
var Pipeline = []*Pass{Expand, Concretize, Select}

func main() {
	cfgPath := flag.String("config", "", "config .sexp file (default: built-in defaults)")
	out := flag.String("out", "out", "output directory for the generated project")
	dump := flag.Bool("dump", false, "write the S-expression after every pass to <out>/.tilegen/")
	strict := flag.Bool("strict", false, "fail on spec forms no tile covers instead of creating LLM tasks")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: tilegen [flags] spec.sexp\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if *showVersion {
		fmt.Println("tilegen", version)
		return
	}
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(flag.Arg(0), *cfgPath, *out, *dump, *strict, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "tilegen:", err)
		os.Exit(1)
	}
}

func run(specPath, cfgPath, out string, dump, strict bool, log io.Writer) error {
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		return err
	}
	src, err := os.ReadFile(specPath)
	if err != nil {
		return err
	}
	nodes, err := Parse(specPath, string(src))
	if err != nil {
		return err
	}
	c := &Ctx{Cfg: cfg, Strict: strict}
	if err := Validate(nodes, c); err != nil {
		return err
	}

	stages := []string{Dump(nodes)}
	for _, p := range Pipeline {
		if nodes, err = p.Run(c, nodes); err != nil {
			return err
		}
		stages = append(stages, Dump(nodes))
	}
	if dump {
		names := []string{"parse"}
		for _, p := range Pipeline {
			names = append(names, p.Name)
		}
		dir := filepath.Join(out, ".tilegen")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		for i, s := range stages {
			f := filepath.Join(dir, fmt.Sprintf("%02d-%s.sexp", i, names[i]))
			if err := os.WriteFile(f, []byte(s), 0o644); err != nil {
				return err
			}
		}
	}

	rep, err := Emit(nodes, out, c)
	for _, w := range c.Warnings {
		fmt.Fprintln(log, "warning:", w)
	}
	if err != nil {
		return err
	}
	for _, f := range rep.Written {
		fmt.Fprintln(log, "  wrote ", f)
	}
	for _, f := range rep.Kept {
		fmt.Fprintln(log, "  kept  ", f, "(yours, not overwritten)")
	}
	fmt.Fprintf(log, "%d open LLM task(s), %d hole(s) already filled -> %s\n",
		rep.Tasks, rep.Done, filepath.Join(out, "tilegen.tasks.json"))
	return nil
}
