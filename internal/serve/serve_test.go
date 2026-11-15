package serve

import (
	"fmt"
	"strings"
	"testing"

	"github.com/vinodhalaharvi/tilegen/internal/compile"
	_ "github.com/vinodhalaharvi/tilegen/internal/tiles" // a spec needs tiles to choose from
)

const notes = `(project notes
  (module example.com/notes)
  (go 1.22)
  (package notes
    (entity Note
      (field ID string)
      (field Text string)
      (store get save (durable)))))
`

// A key is spent only for someone on this machine: the address decides.
func TestIsLoopback(t *testing.T) {
	for addr, want := range map[string]bool{
		"localhost:8080": true,
		"127.0.0.1:8080": true,
		"127.0.0.9:80":   true,
		"[::1]:8080":     true,
		":8080":          false, // every interface
		"0.0.0.0:8080":   false,
		"[::]:8080":      false,
		"192.168.1.5:80": false,
		"example.com:80": false,
		"localhost":      false, // no port: not an address to listen on
		"":               false,
	} {
		if got := IsLoopback(addr); got != want {
			t.Errorf("IsLoopback(%q) = %v", addr, got)
		}
	}
}

func TestParseParts(t *testing.T) {
	forms, err := ParseParts(notes, `(config (storage memory))`, "  ")
	if err != nil {
		t.Fatal(err)
	}
	if len(forms) != 2 || forms[0].Head() != "project" || forms[1].Head() != "config" {
		t.Errorf("the spec, then the config, and nothing for a blank policy: %d forms", len(forms))
	}
	if forms[0].Pos.File != "spec.sexp" || forms[1].Pos.File != "config.sexp" {
		t.Errorf("each part is named for where an error would point: %s, %s", forms[0].Pos.File, forms[1].Pos.File)
	}
	if _, err := ParseParts(notes, "(config (storage", ""); err == nil || !strings.Contains(err.Error(), "config.sexp:1:9: unclosed '('") {
		t.Errorf("an error in the config names the config: %v", err)
	}
	if forms, err := ParseParts("", "", ""); err != nil || len(forms) != 0 {
		t.Errorf("nothing sent: %v, %d forms", err, len(forms))
	}

	// The service takes a spec of a size someone wrote, not a payload.
	var many strings.Builder
	many.WriteString("(project big (module example.com/big) (go 1.22)\n")
	for i := 0; i <= MaxPackages; i++ {
		fmt.Fprintf(&many, "  (package p%d (entity E (field ID string)))\n", i)
	}
	many.WriteString(")")
	if _, err := ParseParts(many.String(), "", ""); err == nil || !strings.Contains(err.Error(), fmt.Sprintf("this spec has %d packages; the service takes up to %d", MaxPackages+1, MaxPackages)) {
		t.Errorf("too many packages: %v", err)
	}
	var wide strings.Builder
	wide.WriteString("(project wide (module example.com/wide) (go 1.22)\n  (package p\n")
	for i := 0; i <= maxItems; i++ {
		fmt.Fprintf(&wide, "    (enum E%d a b)\n", i)
	}
	wide.WriteString("))")
	if _, err := ParseParts(wide.String(), "", ""); err == nil || !strings.Contains(err.Error(), "items; the service takes up to") {
		t.Errorf("too many items: %v", err)
	}
}

// One request in, everything the page shows out: the decisions, the plan
// and the files, or the error and nothing else.
func TestCompileAll(t *testing.T) {
	got := CompileAll(SpecRequest{Spec: notes})
	if got.Error != "" {
		t.Fatal(got.Error)
	}
	if got.Version != compile.Version || got.Name != "notes" || got.Explain == nil || got.Plan == nil {
		t.Errorf("the answer: version %q, name %q", got.Version, got.Name)
	}
	if len(got.Files) == 0 || got.Files["go.mod"] == "" || got.Files[compile.LockFile] == "" {
		t.Errorf("the files: %d", len(got.Files))
	}
	if !strings.Contains(got.Files["go.mod"], "module example.com/notes") {
		t.Errorf("go.mod:\n%s", got.Files["go.mod"])
	}
	holes := 0
	for name, text := range got.Files {
		if strings.HasSuffix(name, ".go") {
			holes += strings.Count(text, `panic("`+compile.HolePrefix)
		}
	}
	if got.Holes == 0 || got.Holes != holes {
		t.Errorf("%d holes reported; the files hold %d", got.Holes, holes)
	}
	if len(got.Next) == 0 || got.Next[len(got.Next)-1] != "go build ./..." {
		t.Errorf("what to run next: %v", got.Next)
	}
	// The same request gives the same answer.
	again := CompileAll(SpecRequest{Spec: notes})
	if len(again.Files) != len(got.Files) {
		t.Fatal("a second answer has a different number of files")
	}
	for name, text := range got.Files {
		if again.Files[name] != text {
			t.Errorf("%s differs the second time", name)
		}
	}

	// A config pins a choice; the files follow it.
	pinned := CompileAll(SpecRequest{Spec: notes, Config: "(config (storage bolt))"})
	if pinned.Error != "" {
		t.Fatal(pinned.Error)
	}
	if _, ok := pinned.Files["notes/bolt_note_store.go"]; !ok {
		t.Errorf("pinned to bolt, the files are: %v", compile.SortedKeys(pinned.Files))
	}

	for name, req := range map[string]SpecRequest{
		"no spec":           {},
		"does not parse":    {Spec: "(project p (module"},
		"no project":        {Spec: "(package p)"},
		"an unknown tile":   {Spec: notes, Config: "(config (storage nosuch))"},
		"a draft malformed": {Spec: notes, Tiles: "(tile x (offers store))"},
	} {
		bad := CompileAll(req)
		if bad.Error == "" || bad.Files != nil || bad.Version != compile.Version {
			t.Errorf("%s: error %q, %d files", name, bad.Error, len(bad.Files))
		}
	}
}
