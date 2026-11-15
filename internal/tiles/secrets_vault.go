package tiles

import (
	"fmt"
	"github.com/vinodhalaharvi/tilegen/internal/compile"
	"strings"
)

func init() {
	compile.RegisterOffer(&compile.Offer{
		Tile:       "vault",
		Engine:     "vault",
		Capability: "secrets",
		Doc:        "one path of a HashiCorp Vault KV v2 mount, read through a short cache",
		Cost: compile.Cost{{Dim: "llm-work", Value: 0}, {Dim: "maintenance", Value: 2, Source: "derived", Note: "a client to keep logged in, and policies to keep in step with the paths"},
			{Dim: "dependency", Value: 3, Source: "derived", Note: "the Vault API client and its module tree"},
			{Dim: "runtime", Value: 1, Source: "derived", Note: "a network read when the cache expires"},
			{Dim: "operations", Value: 5, Source: "derived", Note: "a Vault to run, unseal, back up and upgrade"}},
		Satisfies: map[string]string{
			"rotating": "a read is kept for a ttl you choose, so a new version is seen within it",
			"audited":  "Vault's audit log records every read with the identity that made it",
		},
		Impl: &compile.SecretSource{Suffix: "VaultSecrets", Emit: vaultSecrets,
			Import: [2]string{"vault", "github.com/hashicorp/vault/api"}},
	})
}

func vaultSecrets(pkg string, secrets []compile.Secret) string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }
	w("// VaultSecrets reads secrets from one path of a Vault KV v2 mount, whose\n// keys are:\n//\n%s//\n"+
		"// A read is kept for ttl, so a rotated secret is seen within ttl and Vault\n// is not asked on every call. If Vault cannot be reached once a read is in\n"+
		"// hand, that read keeps being served and Vault is asked again on the next\n// call. Vault's audit log records each read with the identity the client\n// logged in as.\n"+
		"type VaultSecrets struct {\n\tkv   *vault.KVv2\n\tpath string\n\tttl  time.Duration\n\n\tmu      sync.Mutex\n\tdata    map[string]any\n\tfetched time.Time\n}\n\n",
		compile.SecretList(secrets, compile.Secret.ExternalKey))
	w("// NewVaultSecrets reads the path once and checks every key is there, so a\n// wrong path or a missing policy fails when the service starts. The client\n"+
		"// arrives logged in: how this service proves who it is (a token, AppRole,\n// Kubernetes) is decided where the client is made.\n"+
		"func NewVaultSecrets(ctx context.Context, client *vault.Client, mount, path string, ttl time.Duration) (*VaultSecrets, error) {\n"+
		"\ts := &VaultSecrets{kv: client.KVv2(mount), path: path, ttl: ttl}\n\tvar missing []string\n"+
		"\tfor _, key := range []string{%s} {\n\t\tif _, err := s.read(ctx, key); err != nil {\n\t\t\tif s.data == nil {\n\t\t\t\treturn nil, err // the path itself could not be read\n\t\t\t}\n\t\t\tmissing = append(missing, key)\n\t\t}\n\t}\n"+
		"\tif len(missing) > 0 {\n\t\treturn nil, fmt.Errorf(\"%s: vault %%s has no %%s\", path, strings.Join(missing, \", \"))\n\t}\n\treturn s, nil\n}\n\n",
		compile.Quoted(secrets, compile.Secret.ExternalKey), pkg)
	w("func (s *VaultSecrets) read(ctx context.Context, key string) (string, error) {\n\ts.mu.Lock()\n\tdefer s.mu.Unlock()\n"+
		"\tif s.data == nil || time.Since(s.fetched) >= s.ttl {\n\t\tsec, err := s.kv.Get(ctx, s.path)\n"+
		"\t\tswitch {\n\t\tcase err == nil:\n\t\t\ts.data, s.fetched = sec.Data, time.Now()\n"+
		"\t\tcase s.data == nil:\n\t\t\treturn \"\", fmt.Errorf(\"%s: vault %%s: %%w\", s.path, err)\n\t\t}\n\t}\n"+
		"\tv, _ := s.data[key].(string)\n\tif v == \"\" {\n\t\treturn \"\", fmt.Errorf(\"%s: vault %%s has no %%s\", s.path, key)\n\t}\n\treturn v, nil\n}\n", pkg, pkg)
	for _, s := range secrets {
		w("\nfunc (s *VaultSecrets) %s(ctx context.Context) (string, error) { return s.read(ctx, %q) }\n", s.Name, s.ExternalKey())
	}
	return b.String()
}
