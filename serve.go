package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// tilegen serve: a spec in, a project out.
//
// The service is a thin adapter over the same compiler the CLI uses. It
// answers questions about a spec (explain, plan, tiles, check) and returns
// the generated project as a zip.
//
// It never executes anything. Generation writes into a plan in memory
// (Emit with apply=false), which the server zips directly, so no file is
// written and no command is run: no git, no sqlc, no go build. Those
// belong to whoever downloads the zip.
//
// Every request is bounded: a size limit on the body, a timeout, and
// limits on how large a spec may be, because the input is untrusted.

const (
	maxSpecBytes   = 256 << 10 // a spec is a page of text, not a payload
	maxSpecTimeout = 20 * time.Second
	maxPackages    = 50
	maxItems       = 500 // entities, structs, interfaces and the rest, in total
)

func serveCmd(args []string, log io.Writer) error {
	fs := flag.NewFlagSet("tilegen serve", flag.ExitOnError)
	addr := fs.String("addr", defaultAddr(), "address to listen on (Cloud Run sets PORT)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: tilegen serve [-addr :8080]\n\nA spec in, a project out. Executes nothing.\n\n")
		fs.PrintDefaults()
	}
	parseAnywhere(fs, args)

	srvMux := serveMux()
	srv := &http.Server{
		Addr:              *addr,
		Handler:           srvMux,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      maxDraftTimeout + 10*time.Second,
	}
	// With a key set and no address asked for, listen on this machine
	// only: the key is then usable, and never spent for anyone else.
	if key, _ := anthropicKey(); key != "" && !flagGiven(fs, "addr") && os.Getenv("PORT") == "" {
		*addr = "127.0.0.1:8080"
		srv.Addr = *addr
	}
	serveLoopback = isLoopback(*addr)
	fmt.Fprintf(log, "tilegen %s serving on %s\n", version, *addr)
	if st := llmStatus(); st.Enabled {
		fmt.Fprintf(log, "asking Claude to draft tiles is on (%s)\n", st.Model)
	} else {
		fmt.Fprintf(log, "asking Claude to draft tiles is off: %s\n", st.Why)
	}
	return srv.ListenAndServe()
}

// serveMux is every route the service answers, with its limits applied,
// so a test exercises exactly what a request meets.
func serveMux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", handleStudio)
	mux.Handle("GET /assets/", studioAssets())
	mux.HandleFunc("GET /docs", handleIndex)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, "ok") })
	mux.HandleFunc("GET /tiles", handleTiles)
	mux.HandleFunc("POST /explain", handleSpec(explainJSON))
	mux.HandleFunc("POST /plan", handleSpec(planJSONFor))
	mux.HandleFunc("POST /generate", handleGenerate)
	mux.HandleFunc("POST /compile", handleAll)
	mux.HandleFunc("POST /gomods", handleGoMods)
	mux.HandleFunc("GET /version", func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, map[string]any{"version": version, "llm": llmStatus()})
	})
	// Asking Claude takes longer than compiling a spec, so it has its own
	// deadline; everything else keeps the short one.
	limited, draft := withLimits(mux), withTimeout(http.HandlerFunc(handleDraft), maxDraftTimeout)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/draft" {
			draft.ServeHTTP(w, r)
			return
		}
		limited.ServeHTTP(w, r)
	})
}

func defaultAddr() string {
	if p := os.Getenv("PORT"); p != "" { // Cloud Run, and most other hosts
		return ":" + p
	}
	return ":8080"
}

// withLimits caps every request body and gives every handler a deadline,
// so a hostile spec cannot hold a worker open or exhaust memory.
func withLimits(h http.Handler) http.Handler { return withTimeout(h, maxSpecTimeout) }

// withTimeout is withLimits with the deadline given: asking Claude takes
// longer than compiling a spec.
func withTimeout(h http.Handler, d time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxSpecBytes)
		ctx, cancel := context.WithTimeout(r.Context(), d)
		defer cancel()
		w.Header().Set("X-Tilegen-Version", version)
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}

// specRequest is what every endpoint takes: the spec, and optionally a
// config and policy, each as text.
type specRequest struct {
	Spec   string `json:"spec"`
	Config string `json:"config,omitempty"`
	Policy string `json:"policy,omitempty"`
	Name   string `json:"name,omitempty"`
	Tiles  string `json:"tiles,omitempty"` // (tile ...) drafts for this compile only
}

// readSpec accepts either JSON or a raw S-expression body, so curl with a
// file is as easy as a browser with JSON.
func readSpec(r *http.Request) (specRequest, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return specRequest{}, fmt.Errorf("reading the request: %w", err)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return specRequest{}, errors.New("the request body is empty; send a spec")
	}
	var req specRequest
	if ct := r.Header.Get("Content-Type"); strings.Contains(ct, "json") {
		if err := json.Unmarshal(body, &req); err != nil {
			return specRequest{}, fmt.Errorf("the request is not valid JSON: %w", err)
		}
	} else {
		req.Spec = string(body)
	}
	if strings.TrimSpace(req.Spec) == "" {
		return specRequest{}, errors.New(`no spec; send {"spec": "(project ...)"} or the S-expression as the body`)
	}
	return req, nil
}

// compileRequest turns a request into a compiled spec, without touching
// the filesystem: the parser takes text, so the "files" are in memory.
func compileRequest(req specRequest) (*compiled, error) {
	forms, err := parseParts(req.Spec, req.Config, req.Policy)
	if err != nil {
		return nil, err
	}
	return compileForms(forms, Options{Out: "project", Name: req.Name, Tiles: req.Tiles}, io.Discard)
}

// parseParts reads a spec sent as text, with the config and policy that
// may come beside it, into one list of forms.
func parseParts(spec, config, policy string) ([]*Node, error) {
	var forms []*Node
	for _, part := range []struct{ name, text string }{
		{"spec.sexp", spec}, {"config.sexp", config}, {"policy.sexp", policy},
	} {
		if strings.TrimSpace(part.text) == "" {
			continue
		}
		f, err := Parse(part.name, part.text)
		if err != nil {
			return nil, err
		}
		forms = append(forms, f...)
	}
	return forms, checkSpecSize(forms)
}

// checkSpecSize rejects a spec too large to be a person's, before any
// work is done on it.
func checkSpecSize(forms []*Node) error {
	pkgs, items := 0, 0
	for _, f := range forms {
		if f.Head() != "project" && f.Head() != "package" {
			continue
		}
		for _, p := range append([]*Node{f}, f.FindAll("package")...) {
			if p.Head() != "package" {
				continue
			}
			pkgs++
			items += len(p.Args())
		}
	}
	switch {
	case pkgs > maxPackages:
		return fmt.Errorf("this spec has %d packages; the service takes up to %d (the command line has no limit)", pkgs, maxPackages)
	case items > maxItems:
		return fmt.Errorf("this spec has %d items; the service takes up to %d (the command line has no limit)", items, maxItems)
	}
	return nil
}

// handleSpec answers a question about a spec as JSON.
func handleSpec(answer func(*compiled, *Report) any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req, err := readSpec(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		cp, err := compileRequest(req)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, err)
			return
		}
		rep, err := Emit(cp.nodes, cp.o.Out, cp.c, false)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, err)
			return
		}
		respondJSON(w, answer(cp, rep))
	}
}

func explainJSON(cp *compiled, _ *Report) any {
	jsonWeights = cp.c.Policy.Weights
	out := ExplainJSON{Policy: policyJSON(cp.c.Policy)}
	names := make([]string, 0, len(cp.c.Cover))
	for n := range cp.c.Cover {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		out.Coverage = append(out.Coverage, coverageJSON(cp.c.Cover[n]))
	}
	return out
}

func planJSONFor(cp *compiled, rep *Report) any {
	g := buildPlanGraph(rep, cp.c)
	levels, cycleErr := g.Levels()
	return planJSON(g, levels, cycleErr)
}

func handleTiles(w http.ResponseWriter, r *http.Request) { respondJSON(w, tilesJSON(Registry())) }

// usesSqlc reports whether the generated project is finished by running
// sqlc, which is true when tilegen wrote an sqlc.yaml for it.
func usesSqlc(rep *Report) bool {
	for _, f := range append(append([]string{}, rep.Written...), rep.Kept...) {
		if strings.HasSuffix(f, "sqlc.yaml") {
			return true
		}
	}
	return false
}

// handleGenerate returns the project as a zip, built from the plan in
// memory. Nothing is written to disk and nothing is executed.
func handleGenerate(w http.ResponseWriter, r *http.Request) {
	req, err := readSpec(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	cp, err := compileRequest(req)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	rep, err := Emit(cp.nodes, cp.o.Out, cp.c, false)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	name := projectName(cp)
	var buf bytes.Buffer
	if err := writeZip(&buf, name, rep, cp); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name+".zip"))
	w.Header().Set("X-Tilegen-Holes", fmt.Sprint(rep.Tasks))
	// Whether this project is finished by sqlc. A pgx project has a db/
	// folder too - the schema - but no sqlc.yaml and nothing to generate,
	// so "has a db/ folder" is the wrong thing to tell a reader.
	w.Header().Set("X-Tilegen-Sqlc", fmt.Sprint(usesSqlc(rep)))
	w.Write(buf.Bytes())
}

// projectName is what the zip and its top folder are called: the spec's
// project name, or the last element of its module path.
func projectName(cp *compiled) string {
	if cp.o.Name != "" {
		return cp.o.Name
	}
	if m := cp.c.Module; m != "" {
		if i := strings.LastIndex(m, "/"); i >= 0 {
			return m[i+1:]
		}
		return m
	}
	return "project"
}

// writeZip packs the plan, plus a README saying what to do next, since a
// generated project needs sqlc and go mod tidy that the service will not
// run for you.
func writeZip(out io.Writer, name string, rep *Report, cp *compiled) error {
	z := zip.NewWriter(out)
	files := rep.PlanFiles()
	for _, rel := range sortedKeys(files) {
		f, err := z.Create(name + "/" + rel)
		if err != nil {
			return err
		}
		if _, err := f.Write(files[rel]); err != nil {
			return err
		}
	}
	f, err := z.Create(name + "/GENERATED.md")
	if err != nil {
		return err
	}
	if _, err := f.Write(nextSteps(name, rep, cp)); err != nil {
		return err
	}
	return z.Close()
}

func nextSteps(name string, rep *Report, cp *compiled) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\nScaffolded by tilegen %s from your spec.\n\n", name, version)
	b.WriteString("## What this is\n\n")
	b.WriteString("Everything here follows from the spec: the types, the interfaces, the SQL,\n")
	b.WriteString("the routing. What tilegen could not know is left as a typed hole, a method\n")
	b.WriteString("whose body is `panic(\"tilegen:hole ...\")`, with its intent in\n`tilegen.tasks.json`. Fill them with whatever you like.\n\n")
	b.WriteString("## Next\n\n```sh\n")
	for _, st := range toolSteps(cp.c) {
		fmt.Fprintf(&b, "%s        # %s\n", st.Cmd, st.Doc)
	}
	b.WriteString("go mod tidy\ngo build ./...\n```\n\n")
	fmt.Fprintf(&b, "There are %d open hole(s). `tilegen prompt` writes a self-contained prompt\nfor each one; see https://github.com/vinodhalaharvi/tilegen\n", rep.Tasks)
	return []byte(b.String())
}

// respondJSON writes a value as the response body.
func respondJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, v)
}

// writeError reports a failure as JSON. A spec that does not compile is
// the user's to fix, so its message is the compiler's, unchanged.
func writeError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

// everything is one spec compiled once: the choices and why, the plan, and
// every file, as the page shows them together.
type everything struct {
	Version string            `json:"version"`
	Error   string            `json:"error,omitempty"`
	Name    string            `json:"name,omitempty"`
	Explain any               `json:"explain,omitempty"`
	Plan    any               `json:"plan,omitempty"`
	Files   map[string]string `json:"files,omitempty"`
	Holes   int               `json:"holes"`
	Next    []string          `json:"next,omitempty"` // the commands that make it build, in order
	// Drafted is the needs a draft tile won ("orders.OrderStore: cockroachdb").
	// The choices are reported; there are no files, because a draft has
	// no generator.
	Drafted []string `json:"drafted,omitempty"`
}

func compileAll(req specRequest) (out everything) {
	out.Version = version
	defer func() { // a page must get an answer, even to a spec that finds a bug
		if r := recover(); r != nil {
			out = everything{Version: version, Error: fmt.Sprintf("tilegen stopped unexpectedly on this spec (a bug in tilegen): %v", r)}
		}
	}()
	cp, err := compileRequest(req)
	var won *draftsChosen
	if errors.As(err, &won) {
		out.Name, out.Explain, out.Drafted = projectName(won.cp), explainJSON(won.cp, nil), won.needs
		return out
	}
	if err != nil {
		out.Error = err.Error()
		return out
	}
	rep, err := Emit(cp.nodes, cp.o.Out, cp.c, false)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	out.Name = projectName(cp)
	out.Explain, out.Plan = explainJSON(cp, rep), planJSONFor(cp, rep)
	out.Files = map[string]string{}
	for rel, data := range rep.PlanFiles() {
		out.Files[rel] = string(data)
	}
	out.Files["GENERATED.md"] = string(nextSteps(out.Name, rep, cp))
	out.Holes = rep.Tasks
	for _, st := range toolSteps(cp.c) {
		out.Next = append(out.Next, st.Cmd)
	}
	out.Next = append(out.Next, "go mod tidy", "go build ./...")
	return out
}

func handleAll(w http.ResponseWriter, r *http.Request) {
	req, err := readSpec(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	respondJSON(w, compileAll(req))
}

// flagGiven reports whether a flag was set on the command line.
func flagGiven(fs *flag.FlagSet, name string) bool {
	given := false
	fs.Visit(func(f *flag.Flag) { given = given || f.Name == name })
	return given
}
