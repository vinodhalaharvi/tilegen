package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const cockroach = `(tile cockroachdb
  (offers store)
  (doc "distributed SQL that speaks the Postgres protocol")
  (module github.com/cockroachdb/cockroach-go/v2)
  (engine cockroachdb)
  (satisfies (durable) "a write is replicated to a quorum before it is acknowledged")
  (satisfies (cross-process) "any number of clients connect to the cluster")
  (illegal-when (no-broker) "it is a cluster to run")
  (cost (llm-work 2 "plain SQL") (maintenance 2) (dependency 1) (runtime 1) (operations OPS "nodes to run")))`

const draftSpec = `(project n (module example.com/n) (go 1.22)
  (package n (entity Note (field ID string) (store get save (durable) (cross-process)))))`

func compileWith(t *testing.T, spec, tiles string) everything {
	t.Helper()
	return compileAll(specRequest{Spec: spec, Tiles: tiles})
}

func TestADraftTakesPartAndGeneratesNothing(t *testing.T) {
	// Cheap enough to win: the decision is reported with its reasons, and
	// there is no project, because a draft has no generator.
	got := compileWith(t, draftSpec, strings.Replace(cockroach, "OPS", "1", 1))
	if got.Error != "" || len(got.Files) != 0 || len(got.Drafted) != 1 || got.Drafted[0] != "n.NoteStore: cockroachdb" {
		t.Fatalf("a winning draft: error %q, %d files, drafted %v", got.Error, len(got.Files), got.Drafted)
	}
	cov := got.Explain.(ExplainJSON).Coverage[0]
	if cov.Chosen != "cockroachdb" || !cov.Candidates[0].Draft {
		t.Errorf("the explanation should name the draft and mark it: %+v", cov.Candidates[0])
	}
	// Unproven, whoever wrote it: every draft pays uncertainty.
	unproven := false
	for _, term := range cov.Candidates[0].Cost {
		unproven = unproven || (term.Dimension == "uncertainty" && term.Value == draftUnproven)
	}
	if !unproven {
		t.Error("a draft should carry uncertainty for never having run")
	}

	// Too dear to win: it is listed as an alternative, and the project comes
	// from tilegen's own tiles, exactly as without it.
	plain := compileWith(t, draftSpec, "")
	got = compileWith(t, draftSpec, strings.Replace(cockroach, "OPS", "9", 1))
	if got.Error != "" || len(got.Drafted) != 0 || len(got.Files) == 0 {
		t.Fatalf("a losing draft: error %q, drafted %v, %d files", got.Error, got.Drafted, len(got.Files))
	}
	for f, text := range plain.Files {
		if got.Files[f] != text {
			t.Errorf("%s differs because a draft was present and lost", f)
		}
	}
	listed := false
	for _, c := range got.Explain.(ExplainJSON).Coverage[0].Candidates {
		listed = listed || c.Tile == "cockroachdb"
	}
	if !listed {
		t.Error("a losing draft is still an alternative worth showing")
	}

	// It wins only what it claims: a requirement it says nothing about rules it out.
	got = compileWith(t, strings.Replace(draftSpec, "(durable)", "(durable) (lookup-by-field)", 1), strings.Replace(cockroach, "OPS", "0", 1))
	if len(got.Drafted) != 0 {
		t.Fatalf("the draft never claimed lookup-by-field, and won anyway: %v", got.Drafted)
	}
	said := false
	for _, r := range got.Explain.(ExplainJSON).Coverage[0].Illegal {
		said = said || (r.Tile == "cockroachdb" && strings.Contains(r.Reason, "the draft says nothing about it"))
	}
	if !said {
		t.Error("the explanation should say why the draft was ruled out")
	}
}

func TestDraftsAreChecked(t *testing.T) {
	ok := strings.Replace(cockroach, "OPS", "5", 1)
	if _, err := parseDrafts(ok); err != nil {
		t.Fatalf("the example draft should pass: %v", err)
	}
	for name, c := range map[string]struct{ old, new, want string }{
		"upper-case name":     {"(tile cockroachdb", "(tile Cockroach", "lower-case words joined by hyphens"},
		"a built-in's name":   {"(tile cockroachdb", "(tile postgres-sqlc", "tilegen already has a tile named postgres-sqlc"},
		"unknown capability":  {"(offers store)", "(offers stor)", "did you mean store?"},
		"no capability":       {"(offers store)", "", "needs (offers CAPABILITY)"},
		"unknown requirement": {"(durable)", "(durabel)", "did you mean durable?"},
		"a claim with no why": {`(satisfies (cross-process) "any number of clients connect to the cluster")`, "(satisfies (cross-process))", `expected (satisfies (REQUIREMENT) "why")`},
		"two claims on one":   {"(illegal-when (no-broker)", "(illegal-when (durable)", "says two things about durable"},
		"cost out of range":   {"(maintenance 2)", "(maintenance 12)", "a cost is 0 to 9"},
		"unknown dimension":   {"(runtime 1)", "(speed 1)", "speed is not a cost dimension"},
		"a cost left out":     {"(dependency 1)", "", "dependency is missing"},
		"no doc":              {`(doc "distributed SQL that speaks the Postgres protocol")`, "", `needs a (doc "...")`},
		"something else":      {"(engine cockroachdb)", "(port 26257)", "a tile contains offers, doc, module"},
		"not a tile at all":   {"(tile cockroachdb", "(tyle cockroachdb", "expected (tile NAME (offers CAPABILITY) ...)"},
	} {
		if _, err := parseDrafts(strings.Replace(ok, c.old, c.new, 1)); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want an error containing %q, got: %v", name, c.want, err)
		}
	}
	if _, err := parseDrafts(ok + "\n" + ok); err == nil || !strings.Contains(err.Error(), "two drafts are named cockroachdb") {
		t.Errorf("two drafts with one name: %v", err)
	}
	// A bad draft stops the compile with its own message, not a wrong answer.
	if got := compileWith(t, draftSpec, "(tile x (offers store))"); !strings.Contains(got.Error, "tiles.sexp") {
		t.Errorf("a bad draft should be reported: %q", got.Error)
	}
}

// fakeClaude answers the Messages API with the given texts in turn and
// records what it was sent.
func fakeClaude(t *testing.T, answers ...string) (calls *atomic.Int32, last *string) {
	calls, last = &atomic.Int32{}, new(string)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "sk-ant-test" {
			w.WriteHeader(401)
			io.WriteString(w, `{"error":{"type":"authentication_error","message":"invalid x-api-key"}}`)
			return
		}
		b, _ := io.ReadAll(r.Body)
		*last = string(b)
		n := int(calls.Add(1)) - 1
		if n >= len(answers) {
			n = len(answers) - 1
		}
		json.NewEncoder(w).Encode(map[string]any{"content": []map[string]string{{"type": "text", "text": answers[n]}}})
	}))
	old := anthropicURL
	anthropicURL = srv.URL
	t.Cleanup(func() { anthropicURL = old; srv.Close() })
	return calls, last
}

func TestClaudeDraftsAreHeldToTheSameChecks(t *testing.T) {
	ctx := t.Context()
	good := strings.Replace(cockroach, "OPS", "6", 1)
	const mod = "github.com/cockroachdb/cockroach-go/v2"

	calls, last := fakeClaude(t, "```lisp\n"+good+"\n```")
	d := draftModule(ctx, "sk-ant-test", mod, "v2.3.8")
	if d.Error != "" || d.Name != "cockroachdb" || !strings.HasPrefix(d.Tile, "(tile cockroachdb") || calls.Load() != 1 {
		t.Fatalf("a good draft, fenced: %+v after %d call(s)", d, calls.Load())
	}
	// Claude is shown the capabilities, the form and tilegen's own tiles, and the module asked about.
	for _, want := range []string{"Module: " + mod, "(tile postgres-sqlc", "(tile nats-jetstream", "store: persists an entity's values", "(not-a-tile", `"model":"` + defaultModel + `"`} {
		if !strings.Contains(strings.ReplaceAll(*last, `\n`, "\n"), want) && !strings.Contains(*last, want) {
			t.Errorf("the request should carry %q", want)
		}
	}

	// A draft that fails goes back once, with tilegen's reason.
	calls, last = fakeClaude(t, strings.Replace(good, "(durable)", "(durabel)", 1), good)
	d = draftModule(ctx, "sk-ant-test", mod, "v2.3.8")
	if d.Error != "" || d.Name != "cockroachdb" || calls.Load() != 2 || !strings.Contains(*last, "did you mean durable?") {
		t.Fatalf("a corrected draft: %+v after %d call(s)", d, calls.Load())
	}
	// And only once: what still fails is reported, never accepted.
	calls, _ = fakeClaude(t, "I think this is a database.")
	d = draftModule(ctx, "sk-ant-test", mod, "v2.3.8")
	if d.Tile != "" || calls.Load() != 2 || !strings.Contains(d.Error, "did not pass tilegen's checks") {
		t.Fatalf("a draft that never passes: %+v after %d call(s)", d, calls.Load())
	}
	// A tile for some other module is not an answer to this question.
	fakeClaude(t, good)
	if d = draftModule(ctx, "sk-ant-test", "github.com/other/thing", "v1"); d.Tile != "" || !strings.Contains(d.Error, "it was asked about github.com/other/thing") {
		t.Errorf("a draft for the wrong module: %+v", d)
	}
	fakeClaude(t, `(not-a-tile "a payments SDK")`)
	if d = draftModule(ctx, "sk-ant-test", "github.com/stripe/stripe-go/v81", "v81"); d.NotTile != "a payments SDK" || d.Tile != "" || d.Error != "" {
		t.Errorf("not a tile: %+v", d)
	}
	fakeClaude(t, good)
	if d = draftModule(ctx, "sk-ant-wrong", mod, "v2"); !strings.Contains(d.Error, "refused the key") {
		t.Errorf("a bad key should be said plainly: %+v", d)
	}
}

func TestAskingClaudeNeedsAKeyAndALocalServer(t *testing.T) {
	fakeClaude(t, strings.Replace(cockroach, "OPS", "6", 1))
	body := `{"modules":[{"module":"github.com/cockroachdb/cockroach-go/v2","version":"v2.3.8"}]}`
	ask := func() (int, string) {
		res := serveRequest(t, "POST", "/draft", body, "application/json")
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}
	defer func(was bool) { serveLoopback = was }(serveLoopback)

	t.Setenv("ANTHROPIC_API_KEY", "")
	serveLoopback = true
	if code, text := ask(); code != 403 || !strings.Contains(text, "no Anthropic API key is set") {
		t.Errorf("no key: %d %s", code, text)
	}
	// A key on a server other machines can reach is never spent for them.
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")
	serveLoopback = false
	if code, text := ask(); code != 403 || !strings.Contains(text, "a key is never spent for them") {
		t.Errorf("a public server: %d %s", code, text)
	}
	serveLoopback = true
	code, text := ask()
	if code != 200 || !strings.Contains(text, `"name": "cockroachdb"`) && !strings.Contains(text, `"name":"cockroachdb"`) {
		t.Errorf("with a key, on this machine: %d %s", code, text)
	}
	if strings.Contains(text, "sk-ant-test") {
		t.Error("the key must never be in a response")
	}
	// The page learns only whether it is on, and why not.
	res := serveRequest(t, "GET", "/version", "", "")
	b, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(b), `"enabled": true`) || strings.Contains(string(b), "sk-ant") {
		t.Errorf("version: %s", b)
	}
	if res := serveRequest(t, "POST", "/draft", `{"modules":[]}`, "application/json"); res.StatusCode != 400 {
		t.Errorf("no modules should be 400, got %d", res.StatusCode)
	}
	for addr, want := range map[string]bool{"127.0.0.1:8080": true, "localhost:8080": true, "[::1]:8080": true, ":8080": false, "0.0.0.0:8080": false, "192.168.1.5:8080": false} {
		if isLoopback(addr) != want {
			t.Errorf("isLoopback(%q) = %v", addr, !want)
		}
	}
}

func TestBuiltInTilesReadAsDrafts(t *testing.T) {
	// What Claude is shown as examples must itself be a form tilegen accepts.
	for _, pick := range []string{"postgres-sqlc", "nats-jetstream", "vault"} {
		for _, name := range capabilityNames() {
			for _, o := range offersOf(name) {
				if o.Tile != pick {
					continue
				}
				text := strings.Replace(draftForm(o), "(tile "+pick, "(tile example-"+pick, 1)
				if _, err := parseDrafts(text); err != nil {
					t.Errorf("%s, written as a draft, does not pass: %v\n%s", pick, err, text)
				}
			}
		}
	}
}
