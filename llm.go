package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Asking Claude to draft a tile.
//
// A go.mod says a team uses a library. It does not say what the library is
// for, what it can promise or what it costs to run, and those are the
// whole of a tile. That is judgment, the kind tilegen leaves to a person
// or an LLM everywhere else, so this is the same move as `tilegen fill`
// made on a tile instead of a method body.
//
// Three things keep it honest. The draft is checked by the same parser as
// one written by hand, and a draft that fails is sent back once with the
// reason. What comes back is a draft: it wins only what it claims, and it
// pays for being unproven (drafts.go). And nothing is asked unless a key
// is set and the server is listening on this machine only, so a public
// deployment can never spend its owner's key for a stranger.

const (
	defaultModel    = "claude-sonnet-5-5"
	maxDraftModules = 25
	maxDraftTimeout = 120 * time.Second
)

// anthropicURL is where the Messages API is; a test points it elsewhere.
var anthropicURL = "https://api.anthropic.com/v1/messages"

// llmState is whether drafting is available, and why not when it is not.
type llmState struct {
	Enabled bool   `json:"enabled"`
	Why     string `json:"why,omitempty"`
	Model   string `json:"model,omitempty"`
}

var (
	serveLoopback bool // set by serveCmd: the listener is reachable from this machine only
	llmMu         sync.Mutex
)

func llmStatus() llmState {
	key, _ := anthropicKey()
	switch {
	case key == "":
		return llmState{Why: "no Anthropic API key is set: run `tilegen key set`, or set ANTHROPIC_API_KEY, and start serve again"}
	case !serveLoopback:
		return llmState{Why: "this server can be reached from other machines, and a key is never spent for them: start it with -addr localhost:8080"}
	}
	return llmState{Enabled: true, Model: llmModel()}
}

func llmModel() string {
	if m := strings.TrimSpace(os.Getenv("TILEGEN_MODEL")); m != "" {
		return m
	}
	return defaultModel
}

// isLoopback reports whether a listen address accepts connections from
// this machine only.
func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil || host == "" {
		return false // ":8080" is every interface
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// draftForm writes an offer as a (tile ...) form. Claude is shown
// tilegen's own tiles this way, so what it imitates is real.
func draftForm(o *Offer) string {
	var b strings.Builder
	fmt.Fprintf(&b, "(tile %s\n  (offers %s)\n  (doc %q)", o.Tile, o.Capability, o.Doc)
	if o.Module != "" {
		fmt.Fprintf(&b, "\n  (module %s)", o.Module)
	}
	if e := engineOf(o); e != "" && operated(o) {
		fmt.Fprintf(&b, "\n  (engine %s)", e)
	}
	if len(o.Symbols) > 0 {
		fmt.Fprintf(&b, "\n  (symbols %s)", strings.Join(o.Symbols, " "))
	}
	for _, claims := range []struct {
		head string
		m    map[string]string
	}{{"satisfies", o.Satisfies}, {"illegal-when", o.IllegalFor}} {
		for _, req := range capabilities[o.Capability].Requirements {
			if why := claims.m[req]; why != "" {
				fmt.Fprintf(&b, "\n  (%s (%s) %q)", claims.head, req, why)
			}
		}
	}
	b.WriteString("\n  (cost")
	for _, t := range o.Cost {
		if t.Note != "" {
			fmt.Fprintf(&b, "\n    (%s %d %q)", t.Dim, t.Value, t.Note)
		} else {
			fmt.Fprintf(&b, "\n    (%s %d)", t.Dim, t.Value)
		}
	}
	b.WriteString("))")
	return b.String()
}

// draftPrompt is everything Claude is told: the form, what may be offered
// and asked for, three of tilegen's own tiles, and how to say no.
func draftPrompt() string {
	var b strings.Builder
	b.WriteString(`You write tile declarations for tilegen, a compiler that chooses the tools a Go service is built on.

A tile declares what one tool is for, what it can and cannot promise, and what it costs. tilegen gives each need of a service to the cheapest tile that can do everything asked of it, and shows the reason every other tile lost. Those reasons are read by engineers deciding their architecture, so a tile must be accurate and conservative: a claim that turns out false is worse than no tile.

You are given one Go module path from a team's go.mod. Decide whether it shows the team uses a tool one of these capabilities could be offered by, and if so write that tool's tile. A client, driver or helper library for a database, a broker, a secrets manager or an identity provider counts: the tile is for the tool it connects to, reached through this module.

Capabilities, and the requirements a service may ask of each:
`)
	for _, name := range draftCapabilities() {
		c := capabilities[name]
		fmt.Fprintf(&b, "- %s: %s. Requirements: %s\n", c.Name, c.Doc, strings.Join(c.Requirements, ", "))
	}
	b.WriteString(`
The form:

(tile NAME
  (offers CAPABILITY)
  (doc "one line: what it is")
  (module THE/MODULE/PATH)
  (engine NAME)                           ; only if someone has to run a server for it: the server's name
  (symbols pkg.Name pkg.Type.Method ...)  ; the exported symbols your claims rest on
  (satisfies (REQUIREMENT) "how it meets it")
  (illegal-when (REQUIREMENT) "why it cannot")
  (unverified (REQUIREMENT) "what would have to be true")
  (cost (llm-work N "why") (maintenance N "why") (dependency N "why") (runtime N "why") (operations N "why")))

Rules:
- NAME is lower-case words joined by hyphens.
- Say something about every requirement of the capability you choose: satisfies, illegal-when or unverified. Use satisfies only for what the tool guarantees by design, with the mechanism in the reason. If it depends on configuration or you are not sure, use unverified and say what it depends on.
- Each cost is 0 to 9. llm-work is how much code is left for a person or an LLM to write against it; maintenance is what there is to keep working; dependency is how much it pulls in; runtime is its overhead in the process; operations is what someone has to run and look after, and is 0 for a library with no server. Give a reason for any cost that is not obvious.
- When the message lists the module's symbols, they are what the Go type checker read from the module itself at that version. Rest every satisfies on something in that list, cite those symbols in (symbols ...), and cite nothing the list does not contain: tilegen checks each one against the module. When no symbols are listed, leave (symbols ...) out.
- Reasons are plain sentences an engineer can check. No marketing words.

Three of tilegen's own tiles, for the level of detail and the scale of the costs:

`)
	for _, pick := range []string{"postgres-sqlc", "nats-jetstream", "vault"} {
		for _, name := range capabilityNames() {
			for _, o := range offersOf(name) {
				if o.Tile == pick {
					b.WriteString(draftForm(o) + "\n\n")
				}
			}
		}
	}
	b.WriteString(`If the module has nothing to do with any of these capabilities (a utility library, a web framework, an SDK for a business service, a test helper), answer with exactly:

(not-a-tile "one line: what it is instead")

Answer with one form and nothing else: no prose before or after, no code fence.`)
	return b.String()
}

// DraftJSON is what came back for one module.
type DraftJSON struct {
	Module  string `json:"module"`
	Version string `json:"version,omitempty"`
	Tile    string `json:"tile,omitempty"`     // a (tile ...) form that passed tilegen's checks
	Name    string `json:"name,omitempty"`     // its name
	NotTile string `json:"not_tile,omitempty"` // why it is not a tile
	Error   string `json:"error,omitempty"`    // the request failed, or the draft never passed
	// Symbols is how many of the module's exported symbols the draft was
	// written from and checked against. Zero means the module could not
	// be read, FromMemory says why, and the draft is the model's
	// recollection of the library.
	Symbols    int    `json:"symbols,omitempty"`
	FromMemory string `json:"from_memory,omitempty"`
}

// draftSymbolBudget is how much of a module's symbols a draft is shown, in characters.
const draftSymbolBudget = 16000

// symbolsFor reads a module's symbols for a draft, or says why it could not.
func symbolsFor(ctx context.Context, module, version string) (*ModuleSymbols, string) {
	if readSymbols == nil {
		return nil, "this build cannot read modules"
	}
	type result struct {
		m   *ModuleSymbols
		err error
	}
	done := make(chan result, 1)
	go func() {
		m, err := readSymbols(module, version)
		done <- result{m, err}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			return nil, r.err.Error()
		}
		return r.m, ""
	case <-ctx.Done():
		return nil, "reading the module took too long"
	}
}

// askClaude sends one conversation and returns the text of the answer.
func askClaude(ctx context.Context, key, system string, turns []map[string]string) (string, error) {
	body, _ := json.Marshal(map[string]any{"model": llmModel(), "max_tokens": 2000, "system": system, "messages": turns})
	req, err := http.NewRequestWithContext(ctx, "POST", anthropicURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("x-api-key", key)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("could not reach the Anthropic API: %w", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	var out struct {
		Content []struct{ Type, Text string }
		Error   struct{ Type, Message string }
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("the Anthropic API answered %d with something that is not JSON", res.StatusCode)
	}
	if res.StatusCode != 200 {
		switch res.StatusCode {
		case 401:
			return "", errors.New("the Anthropic API refused the key: it may be mistyped, expired or deleted. Set a new one with `tilegen key set`")
		case 429:
			return "", errors.New("the Anthropic API says the key is over its rate or spend limit; try again later")
		}
		return "", fmt.Errorf("the Anthropic API answered %d: %s", res.StatusCode, out.Error.Message)
	}
	var text strings.Builder
	for _, c := range out.Content {
		if c.Type == "text" {
			text.WriteString(c.Text)
		}
	}
	return strings.TrimSpace(text.String()), nil
}

// draftModule asks for one module's tile. A draft that fails tilegen's
// checks goes back once, with the reason.
func draftModule(ctx context.Context, key, module, version string) DraftJSON {
	out := DraftJSON{Module: module, Version: version}
	ask := "Module: " + module + "\nVersion: " + version
	syms, why := symbolsFor(ctx, module, version)
	if syms != nil {
		out.Symbols = len(syms.names())
		ask += "\n\nSymbols:\n" + syms.text(draftSymbolBudget)
	} else {
		out.FromMemory = "the module's symbols could not be read (" + why + "), so this draft is from the model's memory of the library"
	}
	turns := []map[string]string{{"role": "user", "content": ask}}
	system := draftPrompt()
	var problem error
	for attempt := 0; attempt < 2; attempt++ {
		text, err := askClaude(ctx, key, system, turns)
		if err != nil {
			out.Error = err.Error()
			return out
		}
		form := trimFence(text)
		forms, err := Parse("draft.sexp", form)
		switch {
		case err != nil || len(forms) != 1:
			problem = fmt.Errorf("the answer was not one S-expression form")
		case forms[0].Head() == "not-a-tile" && len(forms[0].List) == 2 && !forms[0].List[1].IsList:
			out.NotTile = forms[0].List[1].Atom
			return out
		default:
			var offers []*Offer
			if offers, problem = parseDrafts(form); problem == nil {
				// The module it was asked about is the module it is for.
				if offers[0].Module != "" && offers[0].Module != module {
					problem = fmt.Errorf("the tile's (module ...) is %s, and it was asked about %s", offers[0].Module, module)
				} else if syms == nil && len(offers[0].Symbols) > 0 {
					problem = errors.New("no symbols were listed for this module, so none can be cited: leave (symbols ...) out")
				} else if syms != nil && len(offers[0].Symbols) == 0 {
					problem = errors.New("the module's symbols were listed: cite in (symbols ...) the ones the claims rest on")
				} else if syms != nil && offers[0].Module == "" {
					problem = fmt.Errorf("a tile that cites symbols names its module: add (module %s)", module)
				} else if problem = citedOrNil(offers[0], syms); problem != nil {
				} else {
					out.Tile, out.Name = form, offers[0].Tile
					return out
				}
			}
		}
		turns = append(turns, map[string]string{"role": "assistant", "content": text},
			map[string]string{"role": "user", "content": "tilegen could not accept that:\n" + problem.Error() + "\n\nAnswer again with one corrected form and nothing else."})
	}
	out.Error = "Claude's draft did not pass tilegen's checks after one correction: " + problem.Error()
	return out
}

func citedOrNil(o *Offer, m *ModuleSymbols) error {
	if m == nil {
		return nil
	}
	return checkCited(o, m)
}

// trimFence drops a code fence an answer was wrapped in despite the
// instruction not to.
func trimFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if nl := strings.IndexByte(s, '\n'); nl >= 0 {
			s = s[nl+1:]
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	}
	return strings.TrimSpace(s)
}

// handleDraft answers POST /draft: {"modules": [{"module": "...", "version": "..."}]}.
func handleDraft(w http.ResponseWriter, r *http.Request) {
	st := llmStatus()
	if !st.Enabled {
		writeError(w, http.StatusForbidden, errors.New(st.Why))
		return
	}
	var req struct {
		Modules []struct{ Module, Version string } `json:"modules"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("the request is not valid JSON: %w", err))
		return
	}
	if len(req.Modules) == 0 || len(req.Modules) > maxDraftModules {
		writeError(w, http.StatusBadRequest, fmt.Errorf(`send {"modules": [...]} with 1 to %d modules`, maxDraftModules))
		return
	}
	key, _ := anthropicKey()
	out := make([]DraftJSON, len(req.Modules))
	var wg sync.WaitGroup
	slots := make(chan struct{}, 4) // a few at a time: quick, and gentle on a small rate limit
	for i, m := range req.Modules {
		wg.Add(1)
		go func() {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			out[i] = draftModule(r.Context(), key, strings.TrimSpace(m.Module), strings.TrimSpace(m.Version))
		}()
	}
	wg.Wait()
	respondJSON(w, map[string]any{"model": st.Model, "drafts": out})
}
