package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
)

// tilegen prompt [SPEC] [ID...]
//
// prints one self-contained prompt per task: what to write, the contract,
// intent and constraints, the rules, and the full text of every file the
// task needs. It is built from the same plan as generation and check, so
// it is never stale, and it writes nothing. With no IDs it lists the open
// tasks; -all prints every prompt.

func promptCmd(args []string, stdout, log io.Writer) error {
	fs := flag.NewFlagSet("tilegen prompt", flag.ExitOnError)
	var o Options
	fs.StringVar(&o.Config, "config", "", "config .sexp file (default: a (config ...) form in the spec)")
	fs.StringVar(&o.Out, "out", "", "the generated project (default: the workspace's (out ...), else ./out)")
	fs.StringVar(&o.Name, "name", "", "project name, as given to generation")
	all := fs.Bool("all", false, "print a prompt for every open task")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: tilegen prompt [flags] [SPEC] [ID...]\n\nWith no IDs, lists the open tasks.\n\n")
		fs.PrintDefaults()
	}
	ids := parseAnywhere(fs, args)
	o.Spec = "spec"
	if len(ids) > 0 {
		if _, err := os.Stat(ids[0]); err == nil {
			o.Spec, ids = ids[0], ids[1:]
		}
	}
	cp, err := compile(o, log)
	if err != nil {
		return err
	}
	rep, err := Emit(cp.nodes, cp.o.Out, cp.c, false)
	if err != nil {
		return err
	}
	if len(ids) == 0 && !*all {
		return listTasks(rep.TaskList, cp.o.Spec, stdout)
	}
	byID := map[string]Task{}
	var known []string
	for _, t := range rep.TaskList {
		byID[t.ID] = t
		known = append(known, t.ID)
	}
	selected := rep.TaskList
	if !*all {
		selected = nil
		for _, id := range ids {
			t, ok := byID[id]
			if !ok {
				return fmt.Errorf("no open task %q%s; run `tilegen prompt %s` to list them", id, didYouMean(id, known), cp.o.Spec)
			}
			selected = append(selected, t)
		}
	}
	for i, t := range selected {
		if i > 0 {
			fmt.Fprint(stdout, "\n---\n\n")
		}
		fmt.Fprint(stdout, renderPrompt(t, rep, cp.c))
	}
	return nil
}

func listTasks(tasks []Task, spec string, w io.Writer) error {
	if len(tasks) == 0 {
		fmt.Fprintln(w, "no open tasks")
		return nil
	}
	fmt.Fprintf(w, "%d open task(s). Print one with: tilegen prompt %s ID\n\n", len(tasks), spec)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, t := range tasks {
		where := "(new code)"
		if t.File != "" {
			where = fmt.Sprintf("%s:%d", t.File, t.Line)
		}
		intent := t.Intent
		if len(intent) > 60 {
			intent = intent[:57] + "..."
		}
		status := ""
		if t.Status != "" {
			status = " [" + t.Status + "]"
		}
		fmt.Fprintf(tw, "  %s%s\t%s\t%s\n", t.ID, status, where, intent)
	}
	return tw.Flush()
}

// renderPrompt writes one task as a self-contained Markdown prompt.
func renderPrompt(t Task, r *Report, c *Ctx) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Task %s\n\n", t.ID)
	fmt.Fprintf(&b, "You are writing one piece of a Go project, module `%s`, that tilegen generated from a spec. ", c.Module)
	b.WriteString("Everything around this piece is already written and compiles.\n\n## What to write\n\n")

	decl := ""
	if t.Symbol != "" && t.Contract != "" { // (*EmailSharer).ShareNote -> func (s *EmailSharer) ShareNote(...)
		recv := strings.TrimSuffix(strings.TrimPrefix(t.Symbol, "("), ")."+strings.SplitN(t.Contract, "(", 2)[0])
		decl = fmt.Sprintf("func (s %s) %s", recv, t.Contract)
	}
	switch {
	case t.Status == "drift":
		fmt.Fprintf(&b, "The method `%s` in `%s` (line %d) no longer matches the spec. Change its signature to exactly this, and adapt its body:\n\n```go\n%s\n```\n\n", t.Symbol, t.File, t.Line, decl)
	case t.File != "":
		fmt.Fprintf(&b, "Implement this method in `%s`. Its body is currently `panic(\"%s%s\")` at line %d:\n\n```go\n%s\n```\n\n", t.File, holePrefix, t.ID, t.Line, decl)
	default:
		pkg, _, _ := strings.Cut(t.ID, ".")
		fmt.Fprintf(&b, "Write new Go code in package `%s`, in new files in that package's folder.\n\n", pkg)
		if t.SExpr != "" {
			fmt.Fprintf(&b, "No tile covers this form of the spec, so it is yours to implement:\n\n```lisp\n%s\n```\n\n", t.SExpr)
		}
	}
	fmt.Fprintf(&b, "Intent: %s\n", t.Intent)
	if len(t.Constraints) > 0 {
		b.WriteString("\nConstraints:\n")
		for _, k := range t.Constraints {
			fmt.Fprintf(&b, "- %s\n", k)
		}
	}

	b.WriteString("\n## Rules\n\n")
	if t.File != "" {
		b.WriteString("- Reply with the complete method, signature and body, in a single ```go code block, and nothing else.\n")
		b.WriteString("- Keep the signature exactly as shown.\n")
	} else {
		b.WriteString("- Reply with complete Go files, each in a ```go code block preceded by its path on its own line.\n")
	}
	b.WriteString("- Imports are added automatically; use only the standard library and modules already in go.mod.\n")
	b.WriteString("- Never edit files marked \"Code generated ... DO NOT EDIT\"; they are regenerated from the spec.\n")

	b.WriteString("\n## Context\n")
	files := []string{}
	if t.File != "" {
		files = append(files, t.File)
	}
	files = append(files, t.ContextFiles...)
	files = append(files, "go.mod")
	seen := map[string]bool{}
	for _, f := range files {
		if seen[f] {
			continue
		}
		seen[f] = true
		data, err := r.read(f)
		if err != nil {
			hint := "not generated yet"
			if strings.HasPrefix(f, "internal/db/") {
				hint = "run `sqlc generate` first"
			}
			fmt.Fprintf(&b, "\n### %s\n\n(missing: %s)\n", f, hint)
			continue
		}
		lang := strings.TrimPrefix(filepath.Ext(f), ".")
		if f == "go.mod" {
			lang = ""
		}
		fmt.Fprintf(&b, "\n### %s\n\n```%s\n%s\n```\n", f, lang, strings.TrimRight(string(data), "\n"))
	}
	return b.String()
}
