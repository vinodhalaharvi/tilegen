package main

import (
	"go/parser"
	"go/token"
	"io"
	"strings"
	"testing"
)

func secretsSpec(items string) string {
	return `(project orders (module example.com/orders) (go 1.22)
  (package orders
    (secrets (secret DatabaseURL) (secret SigningKey (doc "signs session tokens.")) ` + items + `)))`
}

// genSecrets generates spec (with cfg, if any) and returns the output
// directory and the tile its secrets got.
func genSecrets(t *testing.T, spec, cfg string) (string, string) {
	t.Helper()
	out := t.TempDir()
	sp, cp := writeSpec(t, t.TempDir(), spec, cfg)
	if err := run(Options{Spec: sp, Config: cp, Out: out}, io.Discard); err != nil {
		t.Fatal(err)
	}
	lock := read(t, out, "tilegen.lock")
	rest := lock[strings.Index(lock, "(tile orders.Secrets ")+len("(tile orders.Secrets "):]
	return out, rest[:strings.Index(rest, ")")]
}

func TestSecretsAreChosenByWhatTheyMustDo(t *testing.T) {
	for reqs, want := range map[string]string{
		"":                     "env-secrets", // nothing to install, nothing to run
		"(rotating)":           "file-secrets",
		"(audited)":            "vault",
		"(rotating) (audited)": "vault",
	} {
		if _, got := genSecrets(t, secretsSpec(reqs), ""); got != want {
			t.Errorf("secrets %s: chose %s, want %s", reqs, got, want)
		}
	}
	// Vault is never the default, but a team that runs one can say so.
	if _, got := genSecrets(t, secretsSpec(""), "(config (secrets vault))"); got != "vault" {
		t.Errorf("the config should choose vault, got %s", got)
	}
	sp, cp := writeSpec(t, t.TempDir(), secretsSpec("(rotating)"), "(config (secrets env))")
	err := run(Options{Spec: sp, Config: cp, Out: t.TempDir()}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "a process's environment is fixed when it starts") {
		t.Errorf("a config cannot choose a tile the requirements rule out: %v", err)
	}
}

func TestSecretsTiles(t *testing.T) {
	iface := []string{
		"type Secrets interface {",
		"\t// DatabaseURL returns the DatabaseURL secret.\n\tDatabaseURL(ctx context.Context) (string, error)",
		"\t// SigningKey signs session tokens.\n\tSigningKey(ctx context.Context) (string, error)",
		"\tOAuthClientID(ctx context.Context) (string, error)",
	}
	for tile, c := range map[string]struct {
		reqs string
		want []string
		not  string
	}{
		"env-secrets": {"", []string{
			"//\tDATABASE_URL\n//\tSIGNING_KEY\n//\tOAUTH_CLIENT_ID\n",
			"func NewEnvSecrets() (*EnvSecrets, error) {",
			`oAuthClientID: os.Getenv("OAUTH_CLIENT_ID"),`,
			`return nil, fmt.Errorf("orders: secrets not set in the environment: %s", strings.Join(missing, ", "))`,
			"func (s *EnvSecrets) DatabaseURL(context.Context) (string, error) { return s.databaseURL, nil }",
			"var _ Secrets = (*EnvSecrets)(nil)",
		}, "hashicorp"},
		"file-secrets": {"(rotating)", []string{
			"//\t<dir>/database-url\n//\t<dir>/signing-key\n//\t<dir>/oauth-client-id\n",
			"func NewFileSecrets(dir string) (*FileSecrets, error) {",
			`for _, name := range []string{"database-url", "signing-key", "oauth-client-id"} {`,
			`func (s *FileSecrets) SigningKey(context.Context) (string, error) { return s.read("signing-key") }`,
			"var _ Secrets = (*FileSecrets)(nil)",
		}, "hashicorp"},
		"vault": {"(audited)", []string{
			`vault "github.com/hashicorp/vault/api"`,
			"func NewVaultSecrets(ctx context.Context, client *vault.Client, mount, path string, ttl time.Duration) (*VaultSecrets, error) {",
			`for _, key := range []string{"database_url", "signing_key", "oauth_client_id"} {`,
			"if s.data == nil || time.Since(s.fetched) >= s.ttl {",
			`func (s *VaultSecrets) OAuthClientID(ctx context.Context) (string, error) {`,
			"var _ Secrets = (*VaultSecrets)(nil)",
		}, "os.Getenv"},
	} {
		out, got := genSecrets(t, secretsSpec("(secret OAuthClientID (key oauth_client_id)) "+c.reqs), "")
		if got != tile {
			t.Fatalf("chose %s, want %s", got, tile)
		}
		src := read(t, out, "orders/secrets_gen.go")
		for _, want := range append(append([]string{}, iface...), c.want...) {
			if !strings.Contains(src, want) {
				t.Errorf("%s: secrets_gen.go missing %q:\n%s", tile, want, src)
			}
		}
		if strings.Contains(src, c.not) {
			t.Errorf("%s: secrets_gen.go should not mention %s", tile, c.not)
		}
		if _, err := parser.ParseFile(token.NewFileSet(), "secrets_gen.go", src, 0); err != nil {
			t.Errorf("%s: does not parse: %v", tile, err)
		}
		// Reading a secret is never a judgment call: nothing is left open.
		if tasks := read(t, out, "tilegen.tasks.json"); strings.Contains(tasks, "Secrets") {
			t.Errorf("%s: secrets should have no holes:\n%s", tile, tasks)
		}
	}
}

func TestSecretsSwitchTileInPlace(t *testing.T) {
	// One file holds the implementation, so changing where secrets come
	// from replaces it and leaves nothing of the old one behind.
	out := t.TempDir()
	gen := func(reqs string) string {
		sp, _ := writeSpec(t, t.TempDir(), secretsSpec(reqs), "")
		if err := run(Options{Spec: sp, Out: out, Reselect: true}, io.Discard); err != nil {
			t.Fatal(err)
		}
		return read(t, out, "orders/secrets_gen.go")
	}
	if src := gen(""); !strings.Contains(src, "EnvSecrets") {
		t.Fatal("want EnvSecrets first")
	}
	if src := gen("(audited)"); strings.Contains(src, "EnvSecrets") || !strings.Contains(src, "VaultSecrets") {
		t.Error("after (audited) the file should hold VaultSecrets and nothing of EnvSecrets")
	}
}

func TestSecretsErrors(t *testing.T) {
	for name, c := range map[string]struct{ items, want string }{
		"a value":         {`(secret APIKey "hunter2")`, "a secret's value never belongs in a spec"},
		"unexported":      {"(secret apiKey)", "must be an exported Go name like DatabaseURL"},
		"twice":           {"(secret DatabaseURL)", "duplicate secret DatabaseURL"},
		"bad key":         {"(secret APIKey (key API-KEY))", "a key is lower-case words joined by underscores"},
		"same key":        {"(secret Other (key signing_key))", "secrets SigningKey and Other would both be read from signing_key"},
		"misspelt need":   {"(rotatin)", "rotating"},
		"unknown form":    {"(password X)", "secrets contain (secret Name)"},
		"another Secrets": {")) (package other (struct Secrets (field X int)) (secrets (secret A)", "the package already declares it"},
	} {
		sp, _ := writeSpec(t, t.TempDir(), secretsSpec(c.items), "")
		if err := run(Options{Spec: sp, Out: t.TempDir()}, io.Discard); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want an error containing %q, got: %v", name, c.want, err)
		}
	}
	sp, _ := writeSpec(t, t.TempDir(), `(project p (module example.com/p) (go 1.22) (package a (secrets (rotating))))`, "")
	if err := run(Options{Spec: sp, Out: t.TempDir()}, io.Discard); err == nil || !strings.Contains(err.Error(), "needs at least one (secret Name)") {
		t.Errorf("an empty (secrets ...) should be refused: %v", err)
	}
}

func TestVaultIsAnEngineOfItsOwn(t *testing.T) {
	// The whole-project view counts a Vault as a server to operate.
	for _, o := range offersOf("secrets") {
		want := "" // the environment and a directory are nothing to run
		if o.Tile == "vault" {
			want = "vault"
		}
		if got := engineOf(o); got != want {
			t.Errorf("%s runs on %q, want %q", o.Tile, got, want)
		}
	}
}
