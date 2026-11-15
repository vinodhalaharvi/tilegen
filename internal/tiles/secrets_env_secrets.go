package tiles

import (
	"fmt"
	"github.com/vinodhalaharvi/tilegen/internal/compile"
	"strings"
)

func init() {
	compile.RegisterOffer(&compile.Offer{
		Tile:       "env-secrets",
		Engine:     compile.NoEngine,
		Capability: "secrets",
		Doc:        "environment variables, read once when the process starts",
		Cost: compile.Cost{{Dim: "llm-work", Value: 0}, {Dim: "maintenance", Value: 1},
			{Dim: "dependency", Value: 0, Source: "derived", Note: "os.Getenv"},
			{Dim: "operations", Value: 0, Source: "derived", Note: "whatever starts the process already sets its environment"}},
		IllegalFor: map[string]string{
			"rotating": "a process's environment is fixed when it starts: a new value is not seen until a restart",
			"audited":  "nothing records which process read a variable, or when",
		},
		Impl: &compile.SecretSource{Suffix: "EnvSecrets", Emit: envSecrets},
	})
	compile.RegisterAlias("env-secrets", "env")
}

func envSecrets(pkg string, secrets []compile.Secret) string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }
	w("// EnvSecrets reads secrets from the environment:\n//\n%s//\n"+
		"// The environment is read once, when the process starts: a changed value\n// takes effect on the next restart.\ntype EnvSecrets struct {\n",
		compile.SecretList(secrets, compile.Secret.Env))
	for _, s := range secrets {
		w("\t%s string\n", s.Field())
	}
	w("}\n\n// NewEnvSecrets reads every secret now and reports all the missing ones\n" +
		"// together, so a misconfigured service fails when it starts rather than on\n// the first request that needs one.\n" +
		"func NewEnvSecrets() (*EnvSecrets, error) {\n\ts := &EnvSecrets{\n")
	for _, s := range secrets {
		w("\t\t%s: os.Getenv(%q),\n", s.Field(), s.Env())
	}
	w("\t}\n\tvar missing []string\n")
	for _, s := range secrets {
		w("\tif s.%s == \"\" {\n\t\tmissing = append(missing, %q)\n\t}\n", s.Field(), s.Env())
	}
	w("\tif len(missing) > 0 {\n\t\treturn nil, fmt.Errorf(\"%s: secrets not set in the environment: %%s\", strings.Join(missing, \", \"))\n\t}\n\treturn s, nil\n}\n", pkg)
	for _, s := range secrets {
		w("\nfunc (s *EnvSecrets) %s(context.Context) (string, error) { return s.%s, nil }\n", s.Name, s.Field())
	}
	return b.String()
}
