package main

import (
	"fmt"
	"go/token"
	"path"
	"regexp"
	"strings"
	"unicode"
)

// (package orders
//   (secrets
//     (secret DatabaseURL)
//     (secret SigningKey (doc "signs session tokens"))
//     (rotating)))
//
// generates a typed Secrets interface, one method per secret, and the
// implementation of whichever tile the requirements leave standing:
//
//   - env-secrets:  the environment, read once when the process starts;
//   - file-secrets: one file per secret in a mounted directory, read on
//                   every call;
//   - vault:        one path of a Vault KV v2 mount, read through a cache.
//
// Nothing here is judgment, so there are no holes. How a service proves
// who it is to Vault is decided where the Vault client is made, and the
// client arrives already logged in.

func init() {
	RegisterCapability(&Capability{
		Name:         "secrets",
		Doc:          "gives a package the values that must never be in the code or the repository",
		Requirements: []string{"rotating", "audited"},
	})

	RegisterOffer(&Offer{
		Tile:       "env-secrets",
		Engine:     noEngine,
		Capability: "secrets",
		Doc:        "environment variables, read once when the process starts",
		Cost: Cost{{Dim: "llm-work", Value: 0}, {Dim: "maintenance", Value: 1},
			{Dim: "dependency", Value: 0, Source: "derived", Note: "os.Getenv"},
			{Dim: "operations", Value: 0, Source: "derived", Note: "whatever starts the process already sets its environment"}},
		IllegalFor: map[string]string{
			"rotating": "a process's environment is fixed when it starts: a new value is not seen until a restart",
			"audited":  "nothing records which process read a variable, or when",
		},
		Impl: &SecretSource{Suffix: "EnvSecrets", Emit: envSecrets},
	})
	registerAlias("env-secrets", "env")

	RegisterOffer(&Offer{
		Tile:       "file-secrets",
		Engine:     noEngine,
		Capability: "secrets",
		Doc:        "one file per secret in a mounted directory, read on every call",
		Cost: Cost{{Dim: "llm-work", Value: 0}, {Dim: "maintenance", Value: 1},
			{Dim: "dependency", Value: 0, Source: "derived", Note: "os.ReadFile"},
			{Dim: "runtime", Value: 1, Source: "derived", Note: "a small file read per call"},
			{Dim: "operations", Value: 1, Source: "derived", Note: "something has to mount the directory: Kubernetes and Docker secrets do"}},
		IllegalFor: map[string]string{
			"audited": "the file system does not record who read a file",
		},
		Satisfies: map[string]string{
			"rotating": "the file is read on every call, so a replaced file is seen at once",
		},
		Impl: &SecretSource{Suffix: "FileSecrets", Emit: fileSecrets},
	})
	registerAlias("file-secrets", "file")

	RegisterOffer(&Offer{
		Tile:       "vault",
		Engine:     "vault",
		Capability: "secrets",
		Doc:        "one path of a HashiCorp Vault KV v2 mount, read through a short cache",
		Cost: Cost{{Dim: "llm-work", Value: 0}, {Dim: "maintenance", Value: 2, Source: "derived", Note: "a client to keep logged in, and policies to keep in step with the paths"},
			{Dim: "dependency", Value: 3, Source: "derived", Note: "the Vault API client and its module tree"},
			{Dim: "runtime", Value: 1, Source: "derived", Note: "a network read when the cache expires"},
			{Dim: "operations", Value: 5, Source: "derived", Note: "a Vault to run, unseal, back up and upgrade"}},
		Satisfies: map[string]string{
			"rotating": "a read is kept for a ttl you choose, so a new version is seen within it",
			"audited":  "Vault's audit log records every read with the identity that made it",
		},
		Impl: &SecretSource{Suffix: "VaultSecrets", Emit: vaultSecrets,
			Import: [2]string{"vault", "github.com/hashicorp/vault/api"}},
	})

	RegisterPackageTile(&PackageTile{
		Form: "secrets",
		Pass: Select,
		Rule: Rule{Name: "secrets", Pattern: Pat("(secrets ?items...)"), Then: selectSecrets,
			Produces: "secrets", Doc: "a typed Secrets interface and its implementation, from wherever the requirements allow",
			Cost: Cost{{Dim: "llm-work", Value: 0}, {Dim: "maintenance", Value: 0}, {Dim: "dependency", Value: 0}}},
		Validate: validateSecrets,
	})
}

var secretKey = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)

// SecretSource is what a secrets offer contributes: the implementation of
// the Secrets interface, written whole.
type SecretSource struct {
	Suffix string    // the type it generates: EnvSecrets
	Import [2]string // qualifier and import path of its client library, if any
	Emit   func(pkg string, secrets []secret) string
}

// secret is one (secret Name (key "...") (doc "...")).
type secret struct{ Name, Doc, Key string }

// key is what the secret is called outside the program: database_url. It
// follows from the name unless the spec gives one, which it needs to for
// a name the rule splits badly: OAuthClientID is o_auth_client_id.
func (s secret) key() string {
	if s.Key != "" {
		return s.Key
	}
	return snake(s.Name)
}
func (s secret) env() string  { return strings.ToUpper(s.key()) }
func (s secret) file() string { return strings.ReplaceAll(s.key(), "_", "-") }
func (s secret) field() string {
	return lowerFirstWord(s.Name)
}

// secretTypes are the names a (secrets ...) form may add to its package.
func secretTypes() []string {
	out := []string{"Secrets"}
	for _, o := range offersOf("secrets") {
		out = append(out, o.Impl.(*SecretSource).Suffix)
	}
	return out
}

func validateSecrets(v *validator, n *Node, types map[string]bool) {
	for _, name := range secretTypes() {
		if types[name] {
			v.bad(n, "(secrets ...) generates %s, but the package already declares it (one secrets form per package)", name)
		}
		types[name] = true
	}
	seen, keys := map[string]bool{}, map[string]string{}
	for _, it := range n.Args() {
		if it.IsList && len(it.List) == 1 {
			validateRequirements("secrets", []string{it.Head()}, it, v)
			continue
		}
		switch it.Head() {
		case "doc":
			v.shape(it, "(doc ?text)")
		case "secret":
			if len(it.List) < 2 || it.List[1].IsList {
				v.bad(it, "expected (secret Name), got %s", short(it))
				continue
			}
			name := it.List[1].Atom
			switch {
			case !token.IsIdentifier(name) || !unicode.IsUpper([]rune(name)[0]):
				v.bad(it, "a secret's name becomes a method of Secrets, so it must be an exported Go name like DatabaseURL, got %q", name)
			case seen[name]:
				v.bad(it, "duplicate secret %s", name)
			}
			seen[name] = true
			sec := secret{Name: name}
			for _, extra := range it.List[2:] {
				switch extra.Head() {
				case "doc":
					v.shape(extra, "(doc ?text)")
				case "key":
					if b := v.shape(extra, "(key ?key)"); b != nil {
						if sec.Key = b.Atom("key"); !secretKey.MatchString(sec.Key) {
							v.bad(extra, "a key is lower-case words joined by underscores, like oauth_client_id, got %q; it becomes OAUTH_CLIENT_ID in the environment and oauth-client-id as a file", sec.Key)
						}
					}
				default:
					v.bad(extra, "a secret has a name, and optionally (key ...) and (doc ...), got %s; a secret's value never belongs in a spec", short(extra))
				}
			}
			if prev := keys[sec.key()]; prev != "" {
				v.bad(it, "secrets %s and %s would both be read from %s; give one a (key ...)", prev, name, sec.key())
			}
			keys[sec.key()] = name
		default:
			v.bad(it, "secrets contain (secret Name), (doc ...) and requirements like (rotating), got %s%s",
				short(it), didYouMean(it.Head(), append([]string{"secret", "doc"}, capabilities["secrets"].Requirements...)))
		}
	}
	if len(seen) == 0 {
		v.bad(n, "(secrets ...) needs at least one (secret Name)")
	}
}

func selectSecrets(m *Munch, b Bindings, n *Node) ([]*Node, error) {
	c, pkg := m.C, m.C.Pkg
	cov := c.Cover[pkg.Name+".Secrets"]
	if cov == nil {
		return nil, fmt.Errorf("no tile was chosen for %s.Secrets", pkg.Name)
	}
	src, ok := cov.Offer.Impl.(*SecretSource)
	if !ok {
		return nil, fmt.Errorf("the tile chosen for %s.Secrets does not implement secrets", pkg.Name)
	}
	var secrets []secret
	for _, it := range n.FindAll("secret") {
		secrets = append(secrets, secret{Name: it.List[1].Atom, Doc: it.Text("doc"), Key: it.Text("key")})
	}

	var w strings.Builder
	doc := n.Text("doc")
	if doc == "" {
		doc = fmt.Sprintf("Secrets gives package %s the values that must never be in the code or\nthe repository. Where they come from is its implementation's business.", pkg.Name)
	}
	fmt.Fprintf(&w, "// %s\ntype Secrets interface {\n", strings.ReplaceAll(doc, "\n", "\n// "))
	for _, s := range secrets {
		d := s.Doc
		if d == "" {
			d = "returns the " + s.Name + " secret."
		}
		fmt.Fprintf(&w, "\t// %s %s\n\t%s(ctx context.Context) (string, error)\n", s.Name, strings.ReplaceAll(d, "\n", "\n\t// "), s.Name)
	}
	w.WriteString("}\n\n")
	w.WriteString(src.Emit(pkg.Name, secrets))
	fmt.Fprintf(&w, "\n// Compile-time check that %s satisfies Secrets.\nvar _ Secrets = (*%s)(nil)", src.Suffix, src.Suffix)

	f, err := goFile(c, path.Join(pkg.Dir, "secrets_gen.go"), "generated", "", []*Node{L(Sym("go/raw"), Str(w.String()))})
	if err != nil {
		return nil, err
	}
	if src.Import[0] != "" {
		f.List = append(f.List, L(Sym("import"), Sym(src.Import[0]), Str(src.Import[1])))
	}
	return []*Node{f}, nil
}

// list renders one line per secret for a doc comment: "//\tDATABASE_URL".
func secretList(secrets []secret, name func(secret) string) string {
	var b strings.Builder
	for _, s := range secrets {
		fmt.Fprintf(&b, "//\t%s\n", name(s))
	}
	return b.String()
}

// quoted renders the names as a Go string list: "a", "b".
func quoted(secrets []secret, name func(secret) string) string {
	var out []string
	for _, s := range secrets {
		out = append(out, fmt.Sprintf("%q", name(s)))
	}
	return strings.Join(out, ", ")
}

func envSecrets(pkg string, secrets []secret) string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }
	w("// EnvSecrets reads secrets from the environment:\n//\n%s//\n"+
		"// The environment is read once, when the process starts: a changed value\n// takes effect on the next restart.\ntype EnvSecrets struct {\n",
		secretList(secrets, secret.env))
	for _, s := range secrets {
		w("\t%s string\n", s.field())
	}
	w("}\n\n// NewEnvSecrets reads every secret now and reports all the missing ones\n" +
		"// together, so a misconfigured service fails when it starts rather than on\n// the first request that needs one.\n" +
		"func NewEnvSecrets() (*EnvSecrets, error) {\n\ts := &EnvSecrets{\n")
	for _, s := range secrets {
		w("\t\t%s: os.Getenv(%q),\n", s.field(), s.env())
	}
	w("\t}\n\tvar missing []string\n")
	for _, s := range secrets {
		w("\tif s.%s == \"\" {\n\t\tmissing = append(missing, %q)\n\t}\n", s.field(), s.env())
	}
	w("\tif len(missing) > 0 {\n\t\treturn nil, fmt.Errorf(\"%s: secrets not set in the environment: %%s\", strings.Join(missing, \", \"))\n\t}\n\treturn s, nil\n}\n", pkg)
	for _, s := range secrets {
		w("\nfunc (s *EnvSecrets) %s(context.Context) (string, error) { return s.%s, nil }\n", s.Name, s.field())
	}
	return b.String()
}

func fileSecrets(pkg string, secrets []secret) string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }
	w("// FileSecrets reads each secret from a file in one directory, the way\n// Kubernetes and Docker mount them:\n//\n%s//\n"+
		"// A file is read on every call, so a rotated secret is seen at once. One\n// trailing newline is not part of the secret.\ntype FileSecrets struct{ dir string }\n\n",
		secretList(secrets, func(s secret) string { return "<dir>/" + s.file() }))
	w("// NewFileSecrets checks that every secret's file is there and not empty,\n// and reports all the missing ones together.\n"+
		"func NewFileSecrets(dir string) (*FileSecrets, error) {\n\ts := &FileSecrets{dir: dir}\n\tvar missing []string\n"+
		"\tfor _, name := range []string{%s} {\n\t\tif _, err := s.read(name); err != nil {\n\t\t\tmissing = append(missing, name)\n\t\t}\n\t}\n"+
		"\tif len(missing) > 0 {\n\t\treturn nil, fmt.Errorf(\"%s: secrets missing or empty in %%s: %%s\", dir, strings.Join(missing, \", \"))\n\t}\n\treturn s, nil\n}\n\n",
		quoted(secrets, secret.file), pkg)
	w("func (s *FileSecrets) read(name string) (string, error) {\n\tb, err := os.ReadFile(filepath.Join(s.dir, name))\n"+
		"\tif err != nil {\n\t\treturn \"\", fmt.Errorf(\"%s: secret %%s: %%w\", name, err)\n\t}\n"+
		"\tv := strings.TrimSuffix(strings.TrimSuffix(string(b), \"\\n\"), \"\\r\")\n"+
		"\tif v == \"\" {\n\t\treturn \"\", fmt.Errorf(\"%s: secret %%s is empty\", name)\n\t}\n\treturn v, nil\n}\n", pkg, pkg)
	for _, s := range secrets {
		w("\nfunc (s *FileSecrets) %s(context.Context) (string, error) { return s.read(%q) }\n", s.Name, s.file())
	}
	return b.String()
}

func vaultSecrets(pkg string, secrets []secret) string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }
	w("// VaultSecrets reads secrets from one path of a Vault KV v2 mount, whose\n// keys are:\n//\n%s//\n"+
		"// A read is kept for ttl, so a rotated secret is seen within ttl and Vault\n// is not asked on every call. If Vault cannot be reached once a read is in\n"+
		"// hand, that read keeps being served and Vault is asked again on the next\n// call. Vault's audit log records each read with the identity the client\n// logged in as.\n"+
		"type VaultSecrets struct {\n\tkv   *vault.KVv2\n\tpath string\n\tttl  time.Duration\n\n\tmu      sync.Mutex\n\tdata    map[string]any\n\tfetched time.Time\n}\n\n",
		secretList(secrets, secret.key))
	w("// NewVaultSecrets reads the path once and checks every key is there, so a\n// wrong path or a missing policy fails when the service starts. The client\n"+
		"// arrives logged in: how this service proves who it is (a token, AppRole,\n// Kubernetes) is decided where the client is made.\n"+
		"func NewVaultSecrets(ctx context.Context, client *vault.Client, mount, path string, ttl time.Duration) (*VaultSecrets, error) {\n"+
		"\ts := &VaultSecrets{kv: client.KVv2(mount), path: path, ttl: ttl}\n\tvar missing []string\n"+
		"\tfor _, key := range []string{%s} {\n\t\tif _, err := s.read(ctx, key); err != nil {\n\t\t\tif s.data == nil {\n\t\t\t\treturn nil, err // the path itself could not be read\n\t\t\t}\n\t\t\tmissing = append(missing, key)\n\t\t}\n\t}\n"+
		"\tif len(missing) > 0 {\n\t\treturn nil, fmt.Errorf(\"%s: vault %%s has no %%s\", path, strings.Join(missing, \", \"))\n\t}\n\treturn s, nil\n}\n\n",
		quoted(secrets, secret.key), pkg)
	w("func (s *VaultSecrets) read(ctx context.Context, key string) (string, error) {\n\ts.mu.Lock()\n\tdefer s.mu.Unlock()\n"+
		"\tif s.data == nil || time.Since(s.fetched) >= s.ttl {\n\t\tsec, err := s.kv.Get(ctx, s.path)\n"+
		"\t\tswitch {\n\t\tcase err == nil:\n\t\t\ts.data, s.fetched = sec.Data, time.Now()\n"+
		"\t\tcase s.data == nil:\n\t\t\treturn \"\", fmt.Errorf(\"%s: vault %%s: %%w\", s.path, err)\n\t\t}\n\t}\n"+
		"\tv, _ := s.data[key].(string)\n\tif v == \"\" {\n\t\treturn \"\", fmt.Errorf(\"%s: vault %%s has no %%s\", s.path, key)\n\t}\n\treturn v, nil\n}\n", pkg, pkg)
	for _, s := range secrets {
		w("\nfunc (s *VaultSecrets) %s(ctx context.Context) (string, error) { return s.read(ctx, %q) }\n", s.Name, s.key())
	}
	return b.String()
}
