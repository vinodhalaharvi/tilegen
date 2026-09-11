package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// tilegen.lock pins the backend each store got, so a changed cost or
// weight never silently moves a store and rewrites its implementation.
// It lives in the generated project, committed like go.sum:
//
//	(lock
//	  (store orders.OrderStore (backend postgres))
//	  (store sessions.SessionStore (backend memory)))
//
// With (storage auto) a pinned choice sticks while it stays legal; explain
// shows when auto would now choose differently. A pin that became illegal
// (or names a backend no longer registered) is re-selected with a warning.
// An explicit (storage NAME) wins and is recorded. -reselect ignores pins.

const lockFile = "tilegen.lock"

// LockEntry is one pinned choice.
type LockEntry struct {
	Backend string
	Pos     Pos
}

// loadLock reads the project's tilegen.lock; a missing file is empty.
func loadLock(out string) (map[string]LockEntry, error) {
	path := filepath.Join(out, lockFile)
	src, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]LockEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	forms, err := Parse(path, string(src))
	if err != nil {
		return nil, err
	}
	b := Bindings{}
	if len(forms) != 1 || !Match(Pat("(lock ?entries...)"), forms[0], b) {
		return nil, fmt.Errorf("%s: expected a single (lock ...) form; delete the file to start over", path)
	}
	lock := map[string]LockEntry{}
	var errs []error
	for _, e := range b.Rest("entries") {
		eb := Bindings{}
		if !Match(Pat("(store ?name (backend ?backend))"), e, eb) || eb.One("name").IsList || eb.One("backend").IsList {
			errs = append(errs, fmt.Errorf("%s: expected (store pkg.NameStore (backend NAME)), got %s", e.Pos, short(e)))
			continue
		}
		lock[eb.Atom("name")] = LockEntry{Backend: eb.Atom("backend"), Pos: e.Pos}
	}
	return lock, errors.Join(errs...)
}

// lockText renders the choices as a lock file, sorted by store.
func lockText(choices map[string]*Choice) []byte {
	names := make([]string, 0, len(choices))
	for n := range choices {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("; tilegen.lock: the backend each store got, pinned. Commit this file.\n")
	b.WriteString("; To choose again, delete a line or run tilegen with -reselect.\n")
	b.WriteString("(lock")
	for _, n := range names {
		fmt.Fprintf(&b, "\n  (store %s (backend %s))", n, choices[n].Chosen.Name)
	}
	b.WriteString(")\n")
	return []byte(b.String())
}

// pinned applies a lock entry to a choice under (storage auto). It returns
// false, with a warning, when the pin can no longer be honoured.
func pinned(ch *Choice, e LockEntry, c *Ctx) bool {
	b := lookupBackend(e.Backend)
	if b == nil {
		c.warn(e.Pos, "%s: backend %q is not registered any more; re-selected%s", lockFile, e.Backend, didYouMean(e.Backend, backendNames()))
		return false
	}
	for _, r := range ch.Illegal {
		if r.B == b {
			c.warn(e.Pos, "%s: %s is now illegal for %s (%s); re-selected", lockFile, b.Tile, ch.Store, r.Reason)
			return false
		}
	}
	ch.Chosen, ch.By = b, "lock"
	return true
}
