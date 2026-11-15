package tiles

import (
	"fmt"
	"github.com/vinodhalaharvi/tilegen/internal/compile"
	"strings"
)

func init() {
	compile.RegisterOffer(&compile.Offer{
		Tile:       "file-secrets",
		Engine:     compile.NoEngine,
		Capability: "secrets",
		Doc:        "one file per secret in a mounted directory, read on every call",
		Cost: compile.Cost{{Dim: "llm-work", Value: 0}, {Dim: "maintenance", Value: 1},
			{Dim: "dependency", Value: 0, Source: "derived", Note: "os.ReadFile"},
			{Dim: "runtime", Value: 1, Source: "derived", Note: "a small file read per call"},
			{Dim: "operations", Value: 1, Source: "derived", Note: "something has to mount the directory: Kubernetes and Docker secrets do"}},
		IllegalFor: map[string]string{
			"audited": "the file system does not record who read a file",
		},
		Satisfies: map[string]string{
			"rotating": "the file is read on every call, so a replaced file is seen at once",
		},
		Impl: &compile.SecretSource{Suffix: "FileSecrets", Emit: fileSecrets},
	})
	compile.RegisterAlias("file-secrets", "file")
}

func fileSecrets(pkg string, secrets []compile.Secret) string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }
	w("// FileSecrets reads each secret from a file in one directory, the way\n// Kubernetes and Docker mount them:\n//\n%s//\n"+
		"// A file is read on every call, so a rotated secret is seen at once. One\n// trailing newline is not part of the secret.\ntype FileSecrets struct{ dir string }\n\n",
		compile.SecretList(secrets, func(s compile.Secret) string { return "<dir>/" + s.File() }))
	w("// NewFileSecrets checks that every secret's file is there and not empty,\n// and reports all the missing ones together.\n"+
		"func NewFileSecrets(dir string) (*FileSecrets, error) {\n\ts := &FileSecrets{dir: dir}\n\tvar missing []string\n"+
		"\tfor _, name := range []string{%s} {\n\t\tif _, err := s.read(name); err != nil {\n\t\t\tmissing = append(missing, name)\n\t\t}\n\t}\n"+
		"\tif len(missing) > 0 {\n\t\treturn nil, fmt.Errorf(\"%s: secrets missing or empty in %%s: %%s\", dir, strings.Join(missing, \", \"))\n\t}\n\treturn s, nil\n}\n\n",
		compile.Quoted(secrets, compile.Secret.File), pkg)
	w("func (s *FileSecrets) read(name string) (string, error) {\n\tb, err := os.ReadFile(filepath.Join(s.dir, name))\n"+
		"\tif err != nil {\n\t\treturn \"\", fmt.Errorf(\"%s: secret %%s: %%w\", name, err)\n\t}\n"+
		"\tv := strings.TrimSuffix(strings.TrimSuffix(string(b), \"\\n\"), \"\\r\")\n"+
		"\tif v == \"\" {\n\t\treturn \"\", fmt.Errorf(\"%s: secret %%s is empty\", name)\n\t}\n\treturn v, nil\n}\n", pkg, pkg)
	for _, s := range secrets {
		w("\nfunc (s *FileSecrets) %s(context.Context) (string, error) { return s.read(%q) }\n", s.Name, s.File())
	}
	return b.String()
}
