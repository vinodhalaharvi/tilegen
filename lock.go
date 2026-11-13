package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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
// It also pins the number of every field and enum value a .proto carries:
//
//	(proto orders.Order (number ID 1) (number Total 2) (reserved 3))
//
// A number is a field's identity on the wire, so it must outlive renames
// of its neighbours, reordering, and its own removal: a removed field's
// number is reserved and never handed out again. -reselect chooses tiles
// again but leaves these alone.
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

// ProtoPin is the numbers one message or enum has handed out.
type ProtoPin struct {
	Nums     map[string]int // field or value name -> its number
	Reserved []int          // numbers of names since removed
}

// pin gives each name its number under key: the pinned one where there
// is one, the next unused one otherwise. Names that were pinned and are
// gone have their numbers reserved.
func (c *Ctx) pin(key string, names []string) (map[string]int, []int) {
	out := &ProtoPin{Nums: map[string]int{}}
	old := c.Pins[key]
	if old == nil {
		old = &ProtoPin{}
	}
	max := 0
	for _, n := range old.Nums {
		if n > max {
			max = n
		}
	}
	for _, n := range old.Reserved {
		if n > max {
			max = n
		}
	}
	live := map[string]bool{}
	for _, name := range names {
		live[name] = true
		if n, ok := old.Nums[name]; ok {
			out.Nums[name] = n
			continue
		}
		max++
		out.Nums[name] = max
	}
	out.Reserved = append(out.Reserved, old.Reserved...)
	for name, n := range old.Nums {
		if !live[name] {
			out.Reserved = append(out.Reserved, n)
		}
	}
	sort.Ints(out.Reserved)
	if c.PinsOut == nil {
		c.PinsOut = map[string]*ProtoPin{}
	}
	c.PinsOut[key] = out
	return out.Nums, out.Reserved
}

// loadProtoPin reads (proto KEY (number NAME N)... (reserved N...)).
func loadProtoPin(e *Node) (string, *ProtoPin, error) {
	if len(e.List) < 2 || e.List[1].IsList {
		return "", nil, fmt.Errorf("%s: expected (proto NAME (number FIELD N)...), got %s", e.Pos, short(e))
	}
	pin := &ProtoPin{Nums: map[string]int{}}
	used := map[int]string{}
	claim := func(it *Node, n int, who string) error {
		if n < 1 {
			return fmt.Errorf("%s: %d is not a field number", it.Pos, n)
		}
		if prev, ok := used[n]; ok {
			return fmt.Errorf("%s: number %d is given to both %s and %s", it.Pos, n, prev, who)
		}
		used[n] = who
		return nil
	}
	for _, it := range e.List[2:] {
		switch {
		case it.Head() == "number" && len(it.List) == 3 && !it.List[1].IsList && !it.List[2].IsList:
			n, err := strconv.Atoi(it.List[2].Atom)
			if err != nil {
				return "", nil, fmt.Errorf("%s: expected a number, got %s", it.Pos, it.List[2].Atom)
			}
			if err := claim(it, n, it.List[1].Atom); err != nil {
				return "", nil, err
			}
			pin.Nums[it.List[1].Atom] = n
		case it.Head() == "reserved":
			for _, a := range it.List[1:] {
				n, err := strconv.Atoi(a.Atom)
				if a.IsList || err != nil {
					return "", nil, fmt.Errorf("%s: expected a number, got %s", a.Pos, short(a))
				}
				if err := claim(a, n, "reserved"); err != nil {
					return "", nil, err
				}
				pin.Reserved = append(pin.Reserved, n)
			}
		default:
			return "", nil, fmt.Errorf("%s: expected (number FIELD N) or (reserved N...), got %s", it.Pos, short(it))
		}
	}
	return e.List[1].Atom, pin, nil
}

// loadLock reads the project's tilegen.lock; a missing file is empty.
func loadLock(out string) (map[string]LockEntry, map[string]*ProtoPin, error) {
	lock, pins, err := readLock(out)
	if lock == nil {
		lock = map[string]LockEntry{}
	}
	if pins == nil {
		pins = map[string]*ProtoPin{}
	}
	return lock, pins, err
}

func readLock(out string) (map[string]LockEntry, map[string]*ProtoPin, error) {
	path := filepath.Join(out, lockFile)
	src, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, errors.ErrUnsupported) { // no file, or (in a browser) no file system
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	forms, err := Parse(path, string(src))
	if err != nil {
		return nil, nil, err
	}
	b := Bindings{}
	if len(forms) != 1 || !Match(Pat("(lock ?entries...)"), forms[0], b) {
		return nil, nil, fmt.Errorf("%s: expected a single (lock ...) form; delete the file to start over", path)
	}
	lock, pins := map[string]LockEntry{}, map[string]*ProtoPin{}
	var errs []error
	for _, e := range b.Rest("entries") {
		if e.Head() == "proto" {
			key, pin, err := loadProtoPin(e)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			pins[key] = pin
			continue
		}
		eb := Bindings{}
		if !Match(Pat("(tile ?need ?tile)"), e, eb) || eb.One("need").IsList || eb.One("tile").IsList {
			// A lock written by an older tilegen: say so once and choose
			// again, rather than making the project unbuildable.
			if e.Head() == "store" {
				return nil, nil, staleLock{path}
			}
			errs = append(errs, fmt.Errorf("%s: expected (tile NEED TILE), got %s", e.Pos, short(e)))
			continue
		}
		lock[eb.Atom("need")] = LockEntry{Tile: eb.Atom("tile"), Pos: e.Pos}
	}
	return lock, pins, errors.Join(errs...)
}

// lockText renders the coverings as a lock file, sorted by need.
func lockText(cover map[string]*Covering, pins map[string]*ProtoPin) []byte {
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
	for _, key := range sortedKeys(pins) {
		pin := pins[key]
		names := sortedKeys(pin.Nums)
		sort.SliceStable(names, func(i, j int) bool { return pin.Nums[names[i]] < pin.Nums[names[j]] })
		fmt.Fprintf(&b, "\n  (proto %s", key)
		for _, name := range names {
			fmt.Fprintf(&b, "\n    (number %s %d)", name, pin.Nums[name])
		}
		if len(pin.Reserved) > 0 {
			b.WriteString("\n    (reserved")
			for _, n := range pin.Reserved {
				fmt.Fprintf(&b, " %d", n)
			}
			b.WriteString(")")
		}
		b.WriteString(")")
	}
	b.WriteString(")\n")
	return []byte(b.String())
}

// staleLock is a lock file in a format tilegen no longer writes.
type staleLock struct{ path string }

func (e staleLock) Error() string {
	return e.path + " was written by an older tilegen; choosing again and rewriting it"
}
