package fill

import (
	"strings"
	"testing"

	"github.com/vinodhalaharvi/tilegen/internal/compile"
)

func TestExtractGo(t *testing.T) {
	method := "func (s *Store) Get(id string) (*Note, error) {\n\treturn s.m[id], nil\n}"
	for name, c := range map[string]struct{ reply, want string }{
		"a go fence":                           {"Here it is:\n```go\n" + method + "\n```\nDone.", method},
		"a bare fence":                         {"```\n" + method + "\n```", method},
		"a golang fence":                       {"```golang\n" + method + "\n```", method},
		"no fence":                             {"\n" + method + "\n\n", method},
		"the fence with a func, not the first": {"```sh\ngo build ./...\n```\nthen\n```go\n" + method + "\n```", method},
		"a fence with no func falls back to the reply": {"```go\nx := 1\n```", "```go\nx := 1\n```"},
		"prose that says func, then the fence":         {"Here is the func you asked for:\n```go\n" + method + "\n```", method},
		"nothing":                                      {"", ""},
	} {
		if got := extractGo(c.reply); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", name, got, c.want)
		}
	}
}

const spliceSrc = `package notes

// Get returns a note.
func (s *Store) Get(id string) (*Note, error) {
	panic("tilegen:hole notes.Store.Get")
}

// Save is kept as it is.
func (s *Store) Save(n *Note) error {
	panic("tilegen:hole notes.Store.Save")
}
`

func TestSplice(t *testing.T) {
	const sig = "func(string) (*Note, error)"
	good := "func (s *Store) Get(id string) (*Note, error) {\n\treturn s.m[id], nil\n}"
	out, err := splice([]byte(spliceSrc), good, "Store", "Get", sig)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	for _, want := range []string{"// Get returns a note.\nfunc (s *Store) Get(id string) (*Note, error) {\n\treturn s.m[id], nil\n}", `panic("tilegen:hole notes.Store.Save")`, "// Save is kept as it is."} {
		if !strings.Contains(got, want) {
			t.Errorf("after the splice the file has no %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "notes.Store.Get") {
		t.Error("the hole is still there")
	}
	// A reply that is a whole file, and one with extra declarations, give
	// up only the method asked for.
	whole := "package notes\n\nimport \"errors\"\n\nvar errX = errors.New(\"x\")\n\n" + good + "\n\nfunc helper() {}\n"
	out2, err := splice([]byte(spliceSrc), whole, "Store", "Get", sig)
	if err != nil || string(out2) != got {
		t.Errorf("a whole file as the reply: %v\n%s", err, out2)
	}
	for name, c := range map[string]struct{ code, want string }{
		"not Go":              {"this is not go {", "the reply is not valid Go"},
		"another method":      {"func (s *Store) Save(n *Note) error { return nil }", "the reply has no method Get on Store"},
		"another receiver":    {"func (s *Other) Get(id string) (*Note, error) { return nil, nil }", "the reply has no method Get on Store"},
		"a plain function":    {"func Get(id string) (*Note, error) { return nil, nil }", "the reply has no method Get on Store"},
		"a changed signature": {"func (s *Store) Get(id int) (*Note, error) { return nil, nil }", "the reply changed the signature to func(int) (*Note, error); keep it exactly " + sig},
	} {
		if _, err := splice([]byte(spliceSrc), c.code, "Store", "Get", sig); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want %q, got %v", name, c.want, err)
		}
	}
	if _, err := splice([]byte("package notes\n"), good, "Store", "Get", sig); err == nil || !strings.Contains(err.Error(), "no longer in the file") {
		t.Errorf("a method that is gone from the file: %v", err)
	}
}

func TestExpectedMethod(t *testing.T) {
	recv, name, sig, err := expectedMethod(compile.Task{ID: "notes.Store.Get", Symbol: "(*Store).Get", Contract: "Get(ctx context.Context, id string) (*Note, error)"})
	if err != nil || recv != "Store" || name != "Get" || sig != "func(context.Context, string) (*Note, error)" {
		t.Errorf("a pointer method: %q %q %q %v", recv, name, sig, err)
	}
	recv, name, _, err = expectedMethod(compile.Task{ID: "x", Symbol: "(Mem).Len", Contract: "Len() int"})
	if err != nil || recv != "Mem" || name != "Len" {
		t.Errorf("a value method: %q %q %v", recv, name, err)
	}
	for _, bad := range []compile.Task{{ID: "x"}, {ID: "x", Symbol: "(*S).M"}, {ID: "x", Contract: "M()"}, {ID: "x", Symbol: "(*S).M", Contract: "M(("}} {
		if _, _, _, err := expectedMethod(bad); err == nil {
			t.Errorf("a task without a usable contract was accepted: %+v", bad)
		}
	}
}

func TestMatchesAny(t *testing.T) {
	for _, c := range []struct {
		id   string
		pats []string
		want bool
	}{
		{"notes.Store.Get", nil, true}, // no filter: everything
		{"notes.Store.Get", []string{"notes.Store.Get"}, true},
		{"notes.Store.Get", []string{"notes.*"}, true},
		{"notes.Store.Get", []string{"orders.*", "notes.Store.G?t"}, true},
		{"notes.Store.Get", []string{"orders.*"}, false},
		{"notes.Store.Get", []string{"notes"}, false},
		{"a[b", []string{"a[b"}, true}, // a malformed pattern still matches itself
	} {
		if got := MatchesAny(c.id, c.pats); got != c.want {
			t.Errorf("MatchesAny(%q, %v) = %v", c.id, c.pats, got)
		}
	}
}

func TestBuildOutputLines(t *testing.T) {
	out := "\n# example.com/notes/notes\nnotes/store.go:12:2: undefined: x\nnotes/store.go:14:9: too many arguments\n"
	if got := errorLine(out); got != "notes/store.go:12:2: undefined: x" {
		t.Errorf("errorLine skips the package header: %q", got)
	}
	if got := errorLine("# only a header\n"); got != "# only a header" {
		t.Errorf("errorLine with nothing but a header: %q", got)
	}
	if firstLineOf("  one\ntwo") != "one" || firstLineOf("") != "" {
		t.Error("firstLineOf")
	}
	if orUnset("") != "(no command set)" || orUnset("claude -p") != "claude -p" {
		t.Error("orUnset")
	}
}

func TestAPICacheName(t *testing.T) {
	a, b := apiCacheName("github.com/nats-io/nats.go"), apiCacheName("github.com/nats-io/nats-go")
	if a == b {
		t.Error("two import paths share a cache file")
	}
	if a != apiCacheName("github.com/nats-io/nats.go") {
		t.Error("the name is not stable")
	}
	long := apiCacheName("example.com/" + strings.Repeat("very/long/", 20) + "path")
	for _, n := range []string{a, b, long} {
		if strings.ContainsAny(n, "/\\") || !strings.HasSuffix(n, ".txt") || len(n) > 60+1+8+4 {
			t.Errorf("not a file name: %q", n)
		}
	}
}
