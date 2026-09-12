package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// tilegen.lock pins the backend each store got, so a changed cost or
// weight never silently moves a store and rewrites its implementation.
// It lives in the generated project, committed like go.sum:
//
//	(lock
//	  (tile orders.OrderStore postgres-sqlc)
//	  (tile sharing.Bus local-bus))
//
// With (storage auto) a pinned choice sticks while it stays legal; explain
// shows when auto would now choose differently. A pin that became illegal
// (or names a backend no longer registered) is re-selected with a warning.
// An explicit (storage NAME) wins and is recorded. -reselect ignores pins.

const lockFile = "tilegen.lock"

// LockEntry is one pinned choice: the tile that covered a need.
type LockEntry struct {
	Tile string
	Pos  Pos
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
		if !Match(Pat("(tile ?need ?tile)"), e, eb) || eb.One("need").IsList || eb.One("tile").IsList {
			errs = append(errs, fmt.Errorf("%s: expected (tile NEED TILE), got %s", e.Pos, short(e)))
			continue
		}
		lock[eb.Atom("need")] = LockEntry{Tile: eb.Atom("tile"), Pos: e.Pos}
	}
	return lock, errors.Join(errs...)
}

// lockText renders the coverings as a lock file, sorted by need.
func lockText(cover map[string]*Covering) []byte {
	var b strings.Builder
	b.WriteString("; tilegen.lock: the tile each need got, pinned. Commit this file.\n")
	b.WriteString("; To choose again, delete a line or run tilegen with -reselect.\n")
	b.WriteString("(lock")
	for _, id := range sortedKeys(cover) {
		fmt.Fprintf(&b, "\n  (tile %s %s)", id, cover[id].Offer.Tile)
		for _, k := range cover[id].Children {
			fmt.Fprintf(&b, "\n  (tile %s %s)", k.Need.ID, k.Offer.Tile)
		}
	}
	b.WriteString(")\n")
	return []byte(b.String())
}
