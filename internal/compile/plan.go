package compile

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

func (r *Report) path(rel string) string { return filepath.Join(r.Out, filepath.FromSlash(rel)) }

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
	if _, ok := r.Staged[rel]; !ok {
		r.order = append(r.order, rel)
	}
	r.note(rel, "")
	r.Staged[rel] = data
	delete(r.Unlinked, rel)
	r.Produced[rel] = true
}

// unlink plans rel's deletion.
func (r *Report) unlink(rel string) { r.Unlinked[rel] = true }

// Read returns rel as the plan will leave it.
func (r *Report) Read(rel string) ([]byte, error) {
	if r.Unlinked[rel] {
		return nil, fs.ErrNotExist
	}
	if data, ok := r.Staged[rel]; ok {
		return data, nil
	}
	return os.ReadFile(r.path(rel))
}

func (r *Report) exists(rel string) bool {
	_, err := r.Read(rel)
	return err == nil
}

// finalize sorts the plan into new, changed and unchanged files.
func (r *Report) finalize() {
	for _, rel := range r.order {
		disk, err := os.ReadFile(r.path(rel))
		switch {
		case errors.Is(err, fs.ErrNotExist), errors.Is(err, errors.ErrUnsupported): // nothing there, or (in a browser) no disk at all
			r.New = append(r.New, rel)
		case err != nil || !bytes.Equal(disk, r.Staged[rel]):
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
		if err := os.WriteFile(p, r.Staged[rel], 0o644); err != nil {
			return err
		}
	}
	gone := make([]string, 0, len(r.Unlinked))
	for rel := range r.Unlinked {
		gone = append(gone, rel)
	}
	sort.Strings(gone)
	for _, rel := range gone {
		p := r.path(rel)
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		removeEmptyDirs(filepath.Dir(p), r.Out)
	}
	return nil
}

// PlanFiles is every file the plan would write, by path. The HTTP service
// zips these directly, so nothing is written to disk; the CLI applies
// them instead.
func (r *Report) PlanFiles() map[string][]byte {
	out := make(map[string][]byte, len(r.Staged))
	for rel, data := range r.Staged {
		out[rel] = data
	}
	return out
}
