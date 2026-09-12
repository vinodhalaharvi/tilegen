package main

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// Generation is plan, then apply. Every emitter stages its writes and
// deletions here; reads see the plan first, then the disk. `tilegen SPEC`
// applies the plan; `tilegen check` compares it with the disk and writes
// nothing. The two can never disagree, because they are the same plan.

func (r *Report) path(rel string) string { return filepath.Join(r.out, filepath.FromSlash(rel)) }

// stage records rel's content in the plan.
// note records that a file is part of the plan and who is responsible,
// even when it is kept rather than written.
func (r *Report) note(rel, by string) {
	if r.producedBy == nil {
		r.producedBy = map[string]string{}
	}
	if by != "" {
		r.producedBy[rel] = by
	}
	if !r.inPlan[rel] {
		if r.inPlan == nil {
			r.inPlan = map[string]bool{}
		}
		r.inPlan[rel] = true
		r.planned = append(r.planned, rel)
	}
}

// stage records rel's content in the plan, and which tile staged it.
func (r *Report) stageBy(rel, by string, data []byte) {
	r.stage(rel, data)
	if r.producedBy == nil {
		r.producedBy = map[string]string{}
	}
	if by != "" {
		r.producedBy[rel] = by
	}
}

func (r *Report) stage(rel string, data []byte) {
	if _, ok := r.staged[rel]; !ok {
		r.order = append(r.order, rel)
	}
	r.note(rel, "")
	r.staged[rel] = data
	delete(r.unlinked, rel)
	r.Produced[rel] = true
}

// unlink plans rel's deletion.
func (r *Report) unlink(rel string) { r.unlinked[rel] = true }

// read returns rel as the plan will leave it.
func (r *Report) read(rel string) ([]byte, error) {
	if r.unlinked[rel] {
		return nil, fs.ErrNotExist
	}
	if data, ok := r.staged[rel]; ok {
		return data, nil
	}
	return os.ReadFile(r.path(rel))
}

func (r *Report) exists(rel string) bool {
	_, err := r.read(rel)
	return err == nil
}

// finalize sorts the plan into new, changed and unchanged files.
func (r *Report) finalize() {
	for _, rel := range r.order {
		disk, err := os.ReadFile(r.path(rel))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			r.New = append(r.New, rel)
		case err != nil || !bytes.Equal(disk, r.staged[rel]):
			r.Changed = append(r.Changed, rel)
		default:
			r.Unchanged++
		}
	}
	r.Written = append(append([]string{}, r.New...), r.Changed...)
}

// Apply writes new and changed files, deletes planned deletions, and
// removes folders left empty.
func (r *Report) Apply() error {
	for _, rel := range r.Written {
		p := r.path(rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, r.staged[rel], 0o644); err != nil {
			return err
		}
	}
	gone := make([]string, 0, len(r.unlinked))
	for rel := range r.unlinked {
		gone = append(gone, rel)
	}
	sort.Strings(gone)
	for _, rel := range gone {
		p := r.path(rel)
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		removeEmptyDirs(filepath.Dir(p), r.out)
	}
	return nil
}

// PlanFiles is every file the plan would write, by path. The HTTP service
// zips these directly, so nothing is written to disk; the CLI applies
// them instead.
func (r *Report) PlanFiles() map[string][]byte {
	out := make(map[string][]byte, len(r.staged))
	for rel, data := range r.staged {
		out[rel] = data
	}
	return out
}
