package compile

import (
	"fmt"
	"go/token"
	"path"
	"regexp"
	"strings"
	"unicode"

	"github.com/vinodhalaharvi/tilegen/internal/sexp"
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

	RegisterPackageTile(&PackageTile{
		Form: "secrets",
		Pass: Select,
		Rule: Rule{Name: "secrets", Pattern: sexp.Pat("(secrets ?items...)"), Then: selectSecrets,
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
	Emit   func(pkg string, secrets []Secret) string
}

// Secret is one (secret Name (key "...") (doc "...")).
type Secret struct{ Name, Doc, Key string }

// ExternalKey is what the secret is called outside the program: database_url. It
// follows from the name unless the spec gives one, which it needs to for
// a name the rule splits badly: OAuthClientID is o_auth_client_id.
func (s Secret) ExternalKey() string {
	if s.Key != "" {
		return s.Key
	}
	return Snake(s.Name)
}
func (s Secret) Env() string  { return strings.ToUpper(s.ExternalKey()) }
func (s Secret) File() string { return strings.ReplaceAll(s.ExternalKey(), "_", "-") }
func (s Secret) Field() string {
	return LowerFirstWord(s.Name)
}

// secretTypes are the names a (secrets ...) form may add to its package.
func secretTypes() []string {
	out := []string{"Secrets"}
	for _, o := range OffersOf("secrets") {
		out = append(out, o.Impl.(*SecretSource).Suffix)
	}
	return out
}

func validateSecrets(v *Validator, n *sexp.Node, types map[string]bool) {
	for _, name := range secretTypes() {
		if types[name] {
			v.Bad(n, "(secrets ...) generates %s, but the package already declares it (one secrets form per package)", name)
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
			v.Shape(it, "(doc ?text)")
		case "secret":
			if len(it.List) < 2 || it.List[1].IsList {
				v.Bad(it, "expected (secret Name), got %s", Short(it))
				continue
			}
			name := it.List[1].Atom
			switch {
			case !token.IsIdentifier(name) || !unicode.IsUpper([]rune(name)[0]):
				v.Bad(it, "a secret's name becomes a method of Secrets, so it must be an exported Go name like DatabaseURL, got %q", name)
			case seen[name]:
				v.Bad(it, "duplicate secret %s", name)
			}
			seen[name] = true
			sec := Secret{Name: name}
			for _, extra := range it.List[2:] {
				switch extra.Head() {
				case "doc":
					v.Shape(extra, "(doc ?text)")
				case "key":
					if b := v.Shape(extra, "(key ?key)"); b != nil {
						if sec.Key = b.Atom("key"); !secretKey.MatchString(sec.Key) {
							v.Bad(extra, "a key is lower-case words joined by underscores, like oauth_client_id, got %q; it becomes OAUTH_CLIENT_ID in the environment and oauth-client-id as a file", sec.Key)
						}
					}
				default:
					v.Bad(extra, "a secret has a name, and optionally (key ...) and (doc ...), got %s; a secret's value never belongs in a spec", Short(extra))
				}
			}
			if prev := keys[sec.ExternalKey()]; prev != "" {
				v.Bad(it, "secrets %s and %s would both be read from %s; give one a (key ...)", prev, name, sec.ExternalKey())
			}
			keys[sec.ExternalKey()] = name
		default:
			v.Bad(it, "secrets contain (secret Name), (doc ...) and requirements like (rotating), got %s%s",
				Short(it), DidYouMean(it.Head(), append([]string{"secret", "doc"}, Capabilities["secrets"].Requirements...)))
		}
	}
	if len(seen) == 0 {
		v.Bad(n, "(secrets ...) needs at least one (secret Name)")
	}
}

func selectSecrets(m *Munch, b sexp.Bindings, n *sexp.Node) ([]*sexp.Node, error) {
	c, pkg := m.C, m.C.Pkg
	cov := c.Cover[pkg.Name+".Secrets"]
	if cov == nil {
		return nil, fmt.Errorf("no tile was chosen for %s.Secrets", pkg.Name)
	}
	src, ok := cov.Offer.Impl.(*SecretSource)
	if !ok {
		return nil, fmt.Errorf("the tile chosen for %s.Secrets does not implement secrets", pkg.Name)
	}
	var secrets []Secret
	for _, it := range n.FindAll("secret") {
		secrets = append(secrets, Secret{Name: it.List[1].Atom, Doc: it.Text("doc"), Key: it.Text("key")})
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

	f, err := GoFile(c, path.Join(pkg.Dir, "secrets_gen.go"), "generated", "", []*sexp.Node{sexp.L(sexp.Sym("go/raw"), sexp.Str(w.String()))})
	if err != nil {
		return nil, err
	}
	if src.Import[0] != "" {
		f.List = append(f.List, sexp.L(sexp.Sym("import"), sexp.Sym(src.Import[0]), sexp.Str(src.Import[1])))
	}
	return []*sexp.Node{f}, nil
}

// list renders one line per secret for a doc comment: "//\tDATABASE_URL".
func SecretList(secrets []Secret, name func(Secret) string) string {
	var b strings.Builder
	for _, s := range secrets {
		fmt.Fprintf(&b, "//\t%s\n", name(s))
	}
	return b.String()
}

// Quoted renders the names as a Go string list: "a", "b".
func Quoted(secrets []Secret, name func(Secret) string) string {
	var out []string
	for _, s := range secrets {
		out = append(out, fmt.Sprintf("%q", name(s)))
	}
	return strings.Join(out, ", ")
}
