package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const (
	billingMod = "module github.com/acme/billing\n\ngo 1.22\n\nrequire (\n\tgithub.com/jackc/pgx/v5 v5.7.2\n\tgithub.com/nats-io/nats.go v1.37.0\n\tgithub.com/stripe/stripe-go/v81 v81.0.0\n\tgithub.com/dgraph-io/badger/v4 v4.5.1\n\tgolang.org/x/sync v0.10.0 // indirect\n)\n"
	catalogMod = "module github.com/acme/catalog\n\ngo 1.22\n\nrequire github.com/jackc/pgx/v5 v5.6.0\nrequire github.com/redis/go-redis/v9 v9.7.0\n"
)

func TestGoModsSayWhatIsAlreadyRun(t *testing.T) {
	got := scanGoMods([]string{billingMod, catalogMod, "this is not a go.mod"})
	if strings.Join(got.Modules, " ") != "github.com/acme/billing github.com/acme/catalog" || len(got.Errors) != 1 {
		t.Fatalf("modules %v, errors %v", got.Modules, got.Errors)
	}
	// Postgres first: both services require pgx. Each engine lists every
	// tile that would share it, not only the one whose library was seen.
	want := map[string]string{"postgres": "postgres-pgx postgres-sqlc", "nats": "nats-core nats-jetstream", "redis": "redis-streams"}
	if len(got.Running) != 3 || got.Running[0].Engine != "postgres" || got.Running[0].In != 2 {
		t.Fatalf("running: %+v", got.Running)
	}
	for _, r := range got.Running {
		if strings.Join(r.Tiles, " ") != want[r.Engine] {
			t.Errorf("%s is shared by %v, want %s", r.Engine, r.Tiles, want[r.Engine])
		}
	}
	// badger is a library linked into a program, not a server someone runs,
	// and an indirect requirement is not a choice anyone made.
	for _, r := range got.Running {
		if r.Engine == "badger" {
			t.Error("an embedded store is not something you run")
		}
	}
	var unknown []string
	for _, u := range got.Unknown {
		unknown = append(unknown, u.Module)
	}
	if strings.Join(unknown, " ") != "github.com/stripe/stripe-go/v81" {
		t.Errorf("unknown: %v", unknown)
	}
	// The newest version seen is the one reported.
	for _, k := range got.Known {
		if k.Module == "github.com/jackc/pgx/v5" && (k.Version != "v5.7.2" || k.In != 2) {
			t.Errorf("pgx: %+v", k)
		}
	}
	if strings.Join(got.Engines, " ") != "kafka mongo nats postgres rabbitmq redis vault" {
		t.Errorf("engines someone operates: %v", got.Engines)
	}
}

func TestRunningEnginesCostNothingMoreToOperate(t *testing.T) {
	spec := `(project notes (module example.com/notes) (go 1.22)
  (package notes (events (durable) (event NoteSaved (field ID string)))))`
	chosen := regexp.MustCompile(`chosen  (\S+)\s+score (\d+)`)
	score := func(policy string) (string, int, string) {
		sp, pp := writeSpec(t, t.TempDir(), spec, "")
		args := []string{sp}
		if policy != "" {
			pp = filepath.Join(t.TempDir(), "policy.sexp")
			if err := os.WriteFile(pp, []byte(policy), 0o644); err != nil {
				t.Fatal(err)
			}
			args = []string{"-policy", pp, sp}
		}
		var log strings.Builder
		if err := explainCmd(args, &log, io.Discard); err != nil {
			t.Fatal(err)
		}
		m := chosen.FindStringSubmatch(log.String())
		if m == nil {
			t.Fatalf("no choice in:\n%s", log.String())
		}
		n, _ := strconv.Atoi(m[2])
		return m[1], n, log.String()
	}
	tile, before, _ := score("")
	tile2, after, text := score("(policy (running nats))")
	if tile != "nats-jetstream" || tile2 != "nats-jetstream" {
		t.Fatalf("chose %s then %s", tile, tile2)
	}
	// JetStream declares operations 4 at weight 3: twelve points that a
	// team already running NATS does not pay again.
	if before-after != 12 {
		t.Errorf("score went from %d to %d, want 12 less", before, after)
	}
	if !strings.Contains(text, "ops 0·3") || !strings.Contains(text, "cost    nats-jetstream operations: 0 (policy: you already run nats)") {
		t.Errorf("explain should say why:\n%s", text)
	}
	// It can change the choice: an operated Kafka beats a JetStream that would be new.
	if tile, _, _ := score("(policy (running kafka))"); tile != "kafka" {
		t.Errorf("with Kafka already run, chose %s", tile)
	}
	bad := filepath.Join(t.TempDir(), "policy.sexp")
	os.WriteFile(bad, []byte("(policy (running postgrs))"), 0o644)
	sp, _ := writeSpec(t, t.TempDir(), spec, "")
	if err := explainCmd([]string{"-policy", bad, sp}, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "did you mean postgres?") {
		t.Errorf("an unknown engine should be refused with the nearest one: %v", err)
	}
}

func TestServeGoMods(t *testing.T) {
	body, _ := json.Marshal(map[string][]string{"files": {billingMod}})
	res := serveRequest(t, "POST", "/gomods", string(body), "application/json")
	var got GoModsJSON
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil || res.StatusCode != 200 || len(got.Running) != 2 {
		t.Fatalf("%d %v %+v", res.StatusCode, err, got)
	}
	// No files is still a question: which engines are there to tick?
	res = serveRequest(t, "POST", "/gomods", `{"files":[]}`, "application/json")
	got = GoModsJSON{}
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil || len(got.Engines) == 0 || len(got.Running) != 0 {
		t.Errorf("%v %+v", err, got)
	}
	if res := serveRequest(t, "POST", "/gomods", `nope`, "application/json"); res.StatusCode != 400 {
		t.Errorf("bad JSON should be 400, got %d", res.StatusCode)
	}
}
