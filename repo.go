package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/mod/modfile"
)

// (repo
//   (github owner/name)     ; or just name: the owner is your gh login
//   (visibility private)    ; private (default) | public | internal
//   (description "...")
//   (topics orders billing) ; added to derived topics (go, golang, ...)
//   (license mit))          ; mit (default) | none; optional holder string

// RepoInfo is a parsed (repo ...) form.
type RepoInfo struct {
	Owner, Name   string
	GitHub        bool
	Visibility    string
	Description   string
	License       string
	LicenseHolder string
	Topics        []string // explicit topics only
}

// Full is owner/name, or name when the owner is not known yet.
func (r *RepoInfo) Full() string {
	if r.Owner == "" {
		return r.Name
	}
	return r.Owner + "/" + r.Name
}

var (
	repoNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	ownerRE    = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?$`)
	topicRE    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,49}$`)
)

// parseRepo reads a validated (repo ...) form. project is the project name,
// the default repository name.
func parseRepo(n *Node, project string) *RepoInfo {
	r := &RepoInfo{Name: project, Visibility: "private", License: "mit"}
	if gh := n.Text("github"); gh != "" {
		r.GitHub = true
		if owner, name, ok := strings.Cut(gh, "/"); ok {
			r.Owner, r.Name = owner, name
		} else {
			r.Name = gh
		}
	}
	if v := n.Text("visibility"); v != "" {
		r.Visibility = v
	}
	r.Description = n.Text("description")
	if l := n.Find("license"); l != nil {
		r.License = l.List[1].Atom
		if len(l.List) > 2 {
			r.LicenseHolder = l.List[2].Atom
		}
	}
	if t := n.Find("topics"); t != nil {
		for _, x := range t.Args() {
			r.Topics = append(r.Topics, x.Atom)
		}
	}
	return r
}

func (v *validator) repo(n *Node) {
	seen := map[string]bool{}
	for _, it := range n.Args() {
		h := it.Head()
		if seen[h] {
			v.bad(it, "duplicate (%s ...) in repo", h)
		}
		seen[h] = true
		switch h {
		case "github":
			if b := v.shape(it, "(github ?repo)"); b != nil {
				owner, name, hasOwner := strings.Cut(b.Atom("repo"), "/")
				if !hasOwner {
					owner, name = "", owner
				}
				if (hasOwner && !ownerRE.MatchString(owner)) || !repoNameRE.MatchString(name) {
					v.bad(it, "github must be owner/name or name, got %q", b.Atom("repo"))
				}
			}
		case "visibility":
			if b := v.shape(it, "(visibility ?v)"); b != nil && !contains([]string{"public", "private", "internal"}, b.Atom("v")) {
				v.bad(it, "visibility must be public, private or internal")
			}
		case "description":
			v.shape(it, "(description ?text)")
		case "topics":
			if len(it.Args()) > 17 { // GitHub allows 20; tilegen derives up to 3 more
				v.bad(it, "too many topics (max 17 explicit)")
			}
			for _, t := range it.Args() {
				if t.IsList || !topicRE.MatchString(t.Atom) {
					v.bad(t, "topic %s must be lowercase letters, digits and hyphens, up to 50 characters", t.Flat())
				}
			}
		case "license":
			if b := v.shape(it, "(license ?kind ?holder...)"); b != nil && !contains([]string{"mit", "none"}, b.Atom("kind")) {
				v.bad(it, "license must be mit or none (add other licenses by hand; tilegen never overwrites LICENSE)")
			}
		default:
			v.bad(it, "unknown repo item %s (want github, visibility, description, topics, license)%s", short(it),
				didYouMean(it.Head(), []string{"github", "visibility", "description", "topics", "license"}))
		}
	}
}

// resolveProject applies -name and fills in what the spec left implicit:
// the GitHub owner and a (module ...) derived from the repository. It
// rewrites the tree, so -dump shows the resolved values.
//
// The owner comes from the most stable source available: the spec's own
// (module ...), then the project's committed go.mod, and only on a -git
// run, `gh api user`. So plain generation never needs the network, and
// every machine derives the same module path.
func resolveProject(project *Node, name, out string, needOwner bool, r Runner, c *Ctx) error {
	if name != "" {
		project.List[1] = &Node{Atom: name, Pos: project.List[1].Pos}
	}
	projectName := project.List[1].Atom
	repo := project.Find("repo")
	if repo == nil {
		return nil
	}
	info := parseRepo(repo, projectName)
	if name != "" {
		info.Name = name
	}
	hasModule := project.Find("module") != nil
	modPath, fromGoMod := project.Text("module"), false
	if !hasModule {
		if data, err := os.ReadFile(filepath.Join(out, "go.mod")); err == nil {
			if m := modfile.ModulePath(data); m != "" {
				modPath, fromGoMod = m, true
			}
		}
	}
	if info.GitHub && info.Owner == "" {
		if owner, ok := githubOwner(modPath); ok {
			info.Owner = owner
		} else if needOwner || modPath == "" {
			if !needOwner {
				return fmt.Errorf("%s: (github %s) has no owner, and there is no (module ...) or go.mod to take it from; write (github OWNER/%s), or run `tilegen -git` once to create the repository",
					repo.Pos, info.Name, info.Name)
			}
			login, err := r.Query("", "gh", "api", "user", "--jq", ".login")
			if err != nil || login == "" {
				return fmt.Errorf("%s: need your GitHub login for (github %s) but `gh api user` failed (run `gh auth login`, or write (github owner/%s)): %v",
					repo.Pos, info.Name, info.Name, err)
			}
			info.Owner = login
		}
	}
	if info.GitHub && info.Owner != "" {
		gh := repo.Find("github")
		gh.List[1] = &Node{Atom: info.Full(), Pos: gh.List[1].Pos}
	}
	want := "github.com/" + info.Full()
	insertModule := func(path string) {
		mod := &Node{IsList: true, Pos: repo.Pos, List: []*Node{Sym("module"), Sym(path)}}
		project.List = append(project.List[:2], append([]*Node{mod}, project.List[2:]...)...)
	}
	switch {
	case fromGoMod:
		insertModule(modPath)
		if info.GitHub && info.Owner != "" && modPath != want {
			c.warn(repo.Pos, "go.mod's module %s differs from repository %s; edit go.mod (and imports) to rename the module", modPath, want)
		}
	case !hasModule && info.GitHub:
		insertModule(want)
	case hasModule && info.GitHub && modPath != want:
		c.warn(project.Find("module").Pos, "module %s differs from repository %s, so `go install %s@latest` will not work; delete (module ...) to derive it",
			modPath, want, want)
	}
	return nil
}

// githubOwner extracts OWNER from github.com/OWNER/REPO[/...].
func githubOwner(module string) (string, bool) {
	parts := strings.Split(module, "/")
	if len(parts) >= 3 && parts[0] == "github.com" && parts[1] != "" {
		return parts[1], true
	}
	return "", false
}

// selectRepo is the tile for (repo ...): starter files, created once and
// then yours, plus a (git/repo ...) target form that -git acts on.
func selectRepo(c *Ctx, project, repo *Node) []*Node {
	name := project.List[1].Atom
	info := parseRepo(repo, name)
	topics := []string{"go", "golang", "tilegen"}
	for _, be := range c.Used {
		topics = append(topics, be.Topics...)
	}
	seen := map[string]bool{}
	gr := L(Sym("git/repo"), L(Sym("name"), Str(info.Name)), L(Sym("visibility"), Sym(info.Visibility)))
	if info.GitHub {
		gr.List = append(gr.List, L(Sym("github"), Str(info.Full())))
	}
	if info.Description != "" {
		gr.List = append(gr.List, L(Sym("description"), Str(info.Description)))
	}
	for _, t := range append(topics, info.Topics...) {
		if !seen[t] {
			seen[t] = true
			gr.List = append(gr.List, L(Sym("topic"), Sym(t)))
		}
	}
	file := func(path, content string) *Node {
		return L(Sym("text/file"), Str(path), L(Sym("mode"), Sym("keep")), Str(content))
	}
	out := []*Node{
		file(".gitignore", "# Build output\n/bin/\n*.test\ncoverage.out\n\n# tilegen pass dumps (regenerate with -dump)\n/.tilegen/\n\n# Editors and OS\n.idea/\n.vscode/\n.DS_Store\n"),
		file("README.md", readme(c, project, info)),
		file("Makefile", makefile(c)),
	}
	if info.License == "mit" {
		holder := info.LicenseHolder
		if holder == "" {
			holder = info.Owner
		}
		if holder == "" {
			holder = "The " + name + " authors"
		}
		out = append(out, file("LICENSE", mitLicense(time.Now().Year(), holder)))
	}
	return append(out, gr)
}

func readme(c *Ctx, project *Node, info *RepoInfo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", info.Name)
	if d := project.Text("doc"); d != "" {
		b.WriteString(d + "\n\n")
	} else if info.Description != "" {
		b.WriteString(info.Description + "\n\n")
	}
	fmt.Fprintf(&b, "Module: `%s`\n\n", c.Module)
	b.WriteString("Scaffolded by tilegen. Files marked `DO NOT EDIT` are regenerated from the spec;\n")
	b.WriteString("everything else, including this README, is yours.\n\n## Develop\n\n```sh\n")
	for _, be := range c.Used {
		if be.Generate != "" {
			fmt.Fprintf(&b, "make %-8s # %s\n", generateTarget(be), be.GenerateDoc)
		}
	}
	b.WriteString("make tidy check\n```\n\nOpen implementation tasks for an LLM are listed in `tilegen.tasks.json`.\n")
	return b.String()
}

func makefile(c *Ctx) string {
	var b strings.Builder
	var gen []*Backend
	for _, be := range c.Used {
		if be.Generate != "" {
			gen = append(gen, be)
		}
	}
	b.WriteString(".PHONY: tidy build vet test check")
	for _, be := range gen {
		b.WriteString(" " + generateTarget(be))
	}
	b.WriteString("\n\ntidy:\n\tgo mod tidy\n\nbuild:\n\tgo build ./...\n\nvet:\n\tgo vet ./...\n\ntest:\n\tgo test ./...\n\ncheck: vet test\n")
	for _, be := range gen {
		fmt.Fprintf(&b, "\n%s:\n\t%s\n", generateTarget(be), be.Generate)
	}
	return b.String()
}

func mitLicense(year int, holder string) string {
	return fmt.Sprintf(`MIT License

Copyright (c) %d %s

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
`, year, holder)
}

// Runner runs git and gh. Query is read-only and always runs; Do changes
// something and is only printed under -dry-run.
type Runner interface {
	Query(dir, name string, args ...string) (string, error)
	Do(dir, name string, args ...string) error
}

type execRunner struct {
	log io.Writer
	dry bool
}

func (r execRunner) Query(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func (r execRunner) Do(dir, name string, args ...string) error {
	line := shellLine(dir, name, args)
	if r.dry {
		fmt.Fprintln(r.log, "  would run:", line)
		return nil
	}
	fmt.Fprintln(r.log, "  $", line)
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %v\n%s", line, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func shellLine(dir, name string, args []string) string {
	parts := []string{name}
	for _, a := range args {
		if a == "" || strings.ContainsAny(a, " \t'\"$&|;<>()*?") {
			a = strconv.Quote(a)
		}
		parts = append(parts, a)
	}
	s := strings.Join(parts, " ")
	if dir != "" {
		s = "(cd " + dir + " && " + s + ")"
	}
	return s
}

// gitPrepare runs before any file is written. It returns how the output
// directory came to be a repository: "existing" (already a git repo: use
// as is), "cloned" (the GitHub repo existed: cloned it), or "new".
func gitPrepare(out string, gr *Node, r Runner, dry bool) (string, error) {
	if _, err := os.Stat(filepath.Join(out, ".git")); err == nil {
		return "existing", nil
	}
	if full := gr.Text("github"); full != "" {
		res, err := r.Query("", "gh", "repo", "view", full, "--json", "name")
		switch {
		case err == nil:
			if entries, _ := os.ReadDir(out); len(entries) > 0 {
				return "", fmt.Errorf("github.com/%s exists but %s is not empty and not a git repository; clone into an empty directory", full, out)
			}
			return "cloned", r.Do("", "gh", "repo", "clone", full, out)
		case strings.Contains(res, "Could not resolve to a Repository"):
			// does not exist yet: fall through and create it
		default:
			return "", fmt.Errorf("checking github.com/%s with gh failed (is gh installed and logged in?): %v %s", full, err, res)
		}
	}
	if !dry {
		if err := os.MkdirAll(out, 0o755); err != nil {
			return "", err
		}
	}
	return "new", r.Do(out, "git", "init", "-q", "-b", "main")
}

// gitFinish runs after generation. New repositories get a buildable first
// commit and, with (github ...), a GitHub repository and topics. Existing
// and cloned ones are left for you to review and commit.
func gitFinish(out, mode string, gr *Node, c *Ctx, r Runner, log io.Writer) error {
	full := gr.Text("github")
	var topics []string
	for _, t := range gr.FindAll("topic") {
		topics = append(topics, t.List[1].Atom)
	}
	if mode == "new" {
		for _, be := range c.Used {
			if be.Generate == "" {
				continue
			}
			cmd := strings.Fields(be.Generate)
			if _, err := exec.LookPath(cmd[0]); err == nil {
				if err := r.Do(out, cmd[0], cmd[1:]...); err != nil {
					fmt.Fprintln(log, "warning:", err)
				}
			} else {
				fmt.Fprintf(log, "warning: %s not found; the first commit will not build until you run `make %s tidy`\n", cmd[0], generateTarget(be))
			}
		}
		if err := r.Do(out, "go", "mod", "tidy"); err != nil {
			fmt.Fprintln(log, "warning:", err)
		}
		if err := r.Do(out, "git", "add", "-A"); err != nil {
			return err
		}
		if err := r.Do(out, "git", "commit", "-q", "-m", "Initial scaffold from tilegen"); err != nil {
			return fmt.Errorf("%v\n(set your identity with: git config --global user.name/user.email)", err)
		}
		if full == "" {
			fmt.Fprintf(log, "created local git repository %s (add (repo (github owner/name)) to publish it)\n", out)
			return nil
		}
		args := []string{"repo", "create", full, "--" + gr.Text("visibility"), "--source=.", "--remote=origin", "--push"}
		if d := gr.Text("description"); d != "" {
			args = append(args, "--description", d)
		}
		if err := r.Do(out, "gh", args...); err != nil {
			return err
		}
	}
	if full != "" && len(topics) > 0 {
		if err := r.Do(out, "gh", "repo", "edit", full, "--add-topic", strings.Join(topics, ",")); err != nil {
			return err
		}
	}
	switch mode {
	case "new":
		if full != "" {
			fmt.Fprintf(log, "published https://github.com/%s\n", full)
		}
	default:
		fmt.Fprintf(log, "generated into the %s repository %s; review with `git -C %s status` and commit when ready\n", mode, out, out)
	}
	return nil
}

// generateTarget names the starter Makefile target for a backend's
// generate step after its tool: sqlc generate -> sqlc.
func generateTarget(be *Backend) string { return strings.Fields(be.Generate)[0] }
