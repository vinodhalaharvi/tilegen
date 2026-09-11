package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/tools/imports"
)

// (workspace
//   (fill
//     (command "claude -p")     ; any CLI: reads the prompt on stdin, prints the answer
//     (retries 1)               ; re-ask with the compiler's errors
//     (timeout "5m")
//     (build "go build ./...")))
//
// tilegen fill [SPEC] [ID or pattern...] fills holes one at a time: prompt,
// splice the reply in with go/ast, build, and on failure restore the hole
// and re-ask with the errors. A hole it cannot fill stays a hole, so the
// project never ends up broken.

// FillConfig is how `tilegen fill` talks to an LLM on this machine.
type FillConfig struct {
	Command string
	Retries int
	Timeout time.Duration
	Build   string
}

func defaultFill() FillConfig {
	return FillConfig{Retries: 1, Timeout: 5 * time.Minute, Build: "go build ./..."}
}

func parseFill(n *Node, fc *FillConfig, bad func(*Node, string, ...any)) {
	for _, it := range n.Args() {
		if len(it.List) != 2 || it.List[1].IsList {
			bad(it, "expected (%s value), got %s", it.Head(), short(it))
			continue
		}
		val := it.List[1].Atom
		switch it.Head() {
		case "command":
			fc.Command = val
		case "build":
			fc.Build = val
		case "retries":
			r, err := strconv.Atoi(val)
			if err != nil || r < 0 || r > 5 {
				bad(it, "retries must be 0 to 5, got %s", val)
			}
			fc.Retries = r
		case "timeout":
			d, err := time.ParseDuration(val)
			if err != nil || d <= 0 {
				bad(it, "timeout must be a duration like \"5m\", got %s", val)
			}
			fc.Timeout = d
		default:
			bad(it, "fill takes (command ...), (retries N), (timeout \"5m\") and (build ...), got %s%s",
				short(it), didYouMean(it.Head(), []string{"command", "retries", "timeout", "build"}))
		}
	}
}

func fillCmd(args []string, log io.Writer) error {
	fs := flag.NewFlagSet("tilegen fill", flag.ExitOnError)
	var o Options
	fs.StringVar(&o.Config, "config", "", "config .sexp file (default: a (config ...) form in the spec)")
	fs.StringVar(&o.Out, "out", "", "the generated project (default: the workspace's (out ...), else ./out)")
	fs.StringVar(&o.Name, "name", "", "project name, as given to generation")
	llm := fs.String("llm", "", "LLM command (default: the workspace's (fill (command ...)))")
	retries := fs.Int("retries", -1, "re-asks after a failed attempt (default: the workspace's, else 1)")
	dry := fs.Bool("dry-run", false, "list what would be filled; call nothing, write nothing")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: tilegen fill [flags] [SPEC] [ID or pattern...]\n\nPatterns use path.Match, e.g. 'billing.*'.\n\n")
		fs.PrintDefaults()
	}
	pats := parseAnywhere(fs, args)
	o.Spec = "spec"
	if len(pats) > 0 {
		if _, err := os.Stat(pats[0]); err == nil {
			o.Spec, pats = pats[0], pats[1:]
		}
	}
	cp, err := compile(o, log)
	if err != nil {
		return err
	}
	fc := defaultFill()
	if cp.ws != nil {
		fc = cp.ws.Fill
	}
	if *llm != "" {
		fc.Command = *llm
	}
	if *retries >= 0 {
		fc.Retries = *retries
	}
	out := cp.o.Out

	// Regenerate first, so the code matches the spec before anything is filled.
	rep, err := Emit(cp.nodes, out, cp.c, !*dry)
	if err != nil {
		return err
	}
	var todo []Task
	var skipped []string
	for _, t := range rep.TaskList {
		if !matchesAny(t.ID, pats) {
			continue
		}
		if t.File == "" {
			skipped = append(skipped, t.ID)
			continue
		}
		todo = append(todo, t)
	}
	for _, id := range skipped {
		fmt.Fprintf(log, "  skip   %s: needs new files; use `tilegen prompt %s %s`\n", id, cp.o.Spec, id)
	}
	if len(todo) == 0 {
		fmt.Fprintln(log, "nothing to fill")
		return nil
	}
	if *dry {
		fmt.Fprintf(log, "would fill %d task(s) with: %s\n", len(todo), orUnset(fc.Command))
		for _, t := range todo {
			fmt.Fprintf(log, "  %s  %s:%d\n", t.ID, t.File, t.Line)
		}
		return nil
	}
	if fc.Command == "" {
		return errors.New("no LLM command: pass -llm \"claude -p\", or add (workspace (fill (command \"claude -p\")))")
	}
	// Drifted methods break the build by design (the compile-time check
	// rejects them), so their errors are expected until they are filled;
	// fill them first. Any other build error means the project was broken
	// before filling, and errors could not be told apart.
	pending := map[string]bool{}
	for _, t := range rep.TaskList {
		if t.Status == "drift" {
			pending[driftKey(t)] = true
		}
	}
	sort.SliceStable(todo, func(i, j int) bool { return todo[i].Status == "drift" && todo[j].Status != "drift" })
	if msg, ok := runBuild(out, fc); !ok && !onlyDrift(msg, pending) {
		return fmt.Errorf("the project does not build before filling, so errors could not be told apart; fix these first:\n%s", msg)
	}

	filled, first := 0, 0
	var failed []string
	for _, t := range todo {
		delete(pending, driftKey(t)) // this task's own drift must be gone after filling it
		attempts, err := fillOne(t, out, cp.c, fc, log, pending)
		if err != nil {
			if t.Status == "drift" {
				pending[driftKey(t)] = true // still drifted: keep tolerating it
			}
			failed = append(failed, t.ID)
			fmt.Fprintf(log, "  open   %s: %v (hole restored)\n", t.ID, firstLineOf(err.Error()))
			continue
		}
		filled++
		if attempts == 1 {
			first++
		}
		fmt.Fprintf(log, "  filled %s (attempt %d)\n", t.ID, attempts)
	}

	// Regenerate again so tilegen.tasks.json reflects what is left.
	if rep, err = Emit(cp.nodes, out, cp.c, true); err != nil {
		return err
	}
	fmt.Fprintf(log, "filled %d of %d (%d on the first try); %d open task(s) remain\n", filled, len(todo), first, rep.Tasks)
	if len(failed) > 0 {
		return fmt.Errorf("%d task(s) could not be filled: %s", len(failed), strings.Join(failed, ", "))
	}
	return nil
}

func matchesAny(id string, pats []string) bool {
	if len(pats) == 0 {
		return true
	}
	for _, p := range pats {
		if ok, _ := path.Match(p, id); ok || p == id {
			return true
		}
	}
	return false
}

// fillOne fills a task, returning the number of attempts it took.
func fillOne(t Task, out string, c *Ctx, fc FillConfig, log io.Writer, pending map[string]bool) (int, error) {
	file := filepath.Join(out, filepath.FromSlash(t.File))
	recv, name, want, err := expectedMethod(t)
	if err != nil {
		return 0, err
	}
	var feedback, previous string
	var lastErr error
	for attempt := 1; attempt <= fc.Retries+1; attempt++ {
		orig, err := os.ReadFile(file)
		if err != nil {
			return attempt, err
		}
		disk := &Report{out: out, staged: map[string][]byte{}, unlinked: map[string]bool{}}
		if holes, err := findHolesSrc(t.File, orig); err == nil && holes[t.ID] > 0 {
			t.Line = holes[t.ID] // earlier fills may have moved it
		}
		prompt := renderPrompt(t, disk, c)
		if feedback != "" {
			prompt += "\n## Your previous answer did not work\n\n```\n" + feedback + "\n```\n\nYour previous answer:\n\n```go\n" +
				previous + "\n```\n\nReply again with the corrected method only.\n"
		}
		reply, err := ask(fc, prompt)
		if err != nil {
			return attempt, err // the command itself failed: retrying will not help
		}
		code := extractGo(reply)
		previous = code
		patched, err := splice(orig, code, recv, name, want)
		if err == nil {
			patched, err = imports.Process(file, patched, &imports.Options{Comments: true, TabIndent: true, TabWidth: 8})
		}
		if err != nil {
			feedback, lastErr = err.Error(), err
			if attempt <= fc.Retries {
				fmt.Fprintf(log, "  retry  %s: %s\n", t.ID, firstLineOf(err.Error()))
			}
			continue
		}
		if err := os.WriteFile(file, patched, 0o644); err != nil {
			return attempt, err
		}
		msg, ok := runBuild(out, fc)
		if ok || onlyDrift(msg, pending) {
			return attempt, nil
		}
		os.WriteFile(file, orig, 0o644) // put the hole back
		feedback, lastErr = msg, errors.New("does not compile: "+errorLine(msg))
		if attempt <= fc.Retries {
			fmt.Fprintf(log, "  retry  %s: %s\n", t.ID, errorLine(msg))
		}
	}
	return fc.Retries + 1, lastErr
}

// expectedMethod turns a task into the receiver type, method name and
// signature (types only) the reply must have.
func expectedMethod(t Task) (recv, name, sig string, err error) {
	if t.Symbol == "" || t.Contract == "" {
		return "", "", "", fmt.Errorf("task %s has no method contract", t.ID)
	}
	name, _, _ = strings.Cut(t.Contract, "(")
	recv = strings.TrimSuffix(strings.TrimPrefix(t.Symbol, "("), ")."+name)
	src := fmt.Sprintf("package p\nfunc (s %s) %s {}\n", recv, t.Contract)
	fset := token.NewFileSet()
	af, err := parser.ParseFile(fset, "", src, 0)
	if err != nil {
		return "", "", "", err
	}
	return strings.TrimPrefix(recv, "*"), name, funcType(fset, af.Decls[0].(*ast.FuncDecl).Type), nil
}

var goFence = regexp.MustCompile("(?s)```(?:go|golang)?[ \t]*\n(.*?)```")

// extractGo takes the Go code out of a reply: the first fenced block that
// declares a func, else the whole reply.
func extractGo(reply string) string {
	for _, m := range goFence.FindAllStringSubmatch(reply, -1) {
		if strings.Contains(m[1], "func ") {
			return strings.TrimSpace(m[1])
		}
	}
	return strings.TrimSpace(reply)
}

// splice replaces the method recv.name in src with the one in code, after
// checking that code declares exactly that method with the wanted
// signature. The method's doc comment in src is kept.
func splice(src []byte, code, recv, name, want string) ([]byte, error) {
	body := code
	if !strings.HasPrefix(strings.TrimSpace(code), "package ") {
		body = "package p\n\n" + code
	}
	fset := token.NewFileSet()
	af, err := parser.ParseFile(fset, "reply.go", body, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("the reply is not valid Go: %v", err)
	}
	var got *ast.FuncDecl
	for _, d := range af.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == name && fd.Recv != nil && recvType(fd.Recv.List[0].Type) == recv {
			got = fd
		}
	}
	if got == nil {
		return nil, fmt.Errorf("the reply has no method %s on %s; reply with exactly that method", name, recv)
	}
	if sig := funcType(fset, got.Type); sig != want {
		return nil, fmt.Errorf("the reply changed the signature to %s; keep it exactly %s", sig, want)
	}
	newText := body[fset.Position(got.Pos()).Offset:fset.Position(got.End()).Offset]

	ofset := token.NewFileSet()
	of, err := parser.ParseFile(ofset, "orig.go", src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	for _, d := range of.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == name && fd.Recv != nil && recvType(fd.Recv.List[0].Type) == recv {
			start, end := ofset.Position(fd.Pos()).Offset, ofset.Position(fd.End()).Offset
			return append(append(append([]byte{}, src[:start]...), newText...), src[end:]...), nil
		}
	}
	return nil, fmt.Errorf("method %s on %s is no longer in the file", name, recv)
}

// ask sends the prompt to the configured command on stdin.
func ask(fc FillConfig, prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), fc.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", fc.Command)
	cmd.Stdin = strings.NewReader(prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		return "", fmt.Errorf("%q timed out after %s", fc.Command, fc.Timeout)
	case err != nil:
		return "", fmt.Errorf("%q failed: %v: %s", fc.Command, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// runBuild runs the build command in the project; it returns the output
// (trimmed to what fits in a prompt) and whether it succeeded.
func runBuild(out string, fc FillConfig) (string, bool) {
	cmd := exec.Command("sh", "-c", fc.Build)
	cmd.Dir = out
	b, err := cmd.CombinedOutput()
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) > 40 {
		lines = append(lines[:40], "...")
	}
	return strings.Join(lines, "\n"), err == nil
}

func firstLineOf(s string) string { l, _, _ := strings.Cut(strings.TrimSpace(s), "\n"); return l }

// errorLine is the first real error in build output, skipping the
// "# package" headers go build prints.
func errorLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
			return l
		}
	}
	return firstLineOf(s)
}

func orUnset(s string) string {
	if s == "" {
		return "(no command set)"
	}
	return s
}

// driftKey names a drifted method as the build reports it: Type.Method.
func driftKey(t Task) string {
	if t.Status != "drift" {
		return ""
	}
	recv, name, _, err := expectedMethod(t)
	if err != nil {
		return ""
	}
	return recv + "." + name
}

var driftErr = regexp.MustCompile(`\*?(\w+) does not implement \S+ \(wrong type for method (\w+)\)`)

// onlyDrift reports whether every error in build output is a compile-time
// interface check failing for a method whose drift task is still pending.
func onlyDrift(msg string, pending map[string]bool) bool {
	if len(pending) == 0 {
		return false
	}
	errors := 0
	for _, l := range strings.Split(msg, "\n") {
		if l == "" || strings.HasPrefix(l, "#") || strings.HasPrefix(l, "\t") || strings.HasPrefix(l, " ") {
			continue // package headers and have/want continuation lines
		}
		errors++
		m := driftErr.FindStringSubmatch(l)
		if m == nil || !pending[m[1]+"."+m[2]] {
			return false
		}
	}
	return errors > 0
}
