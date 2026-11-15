package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/vinodhalaharvi/tilegen/internal/importer"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// maxDraftModules bounds how many modules one request may ask about.
const maxDraftModules = 25

// maxDraftTimeout bounds one drafting request.
const maxDraftTimeout = 120 * time.Second

// llmState is whether drafting is available, and why not when it is not.
type llmState struct {
	Enabled bool   `json:"enabled"`
	Why     string `json:"why,omitempty"`
	Model   string `json:"model,omitempty"`
}

// serveLoopback is set by serveCmd: the listener is reachable from this machine only.
var serveLoopback bool

func llmStatus() llmState {
	key, _ := anthropicKey()
	switch {
	case key == "":
		return llmState{Why: "no Anthropic API key is set: run `tilegen key set`, or set ANTHROPIC_API_KEY, and start serve again"}
	case !serveLoopback:
		return llmState{Why: "this server can be reached from other machines, and a key is never spent for them: start it with -addr localhost:8080"}
	}
	return llmState{Enabled: true, Model: importer.Model()}
}

// isLoopback reports whether a listen address accepts connections from
// this machine only.
func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil || host == "" {
		return false // ":8080" is every interface
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// handleDraft answers POST /draft: {"modules": [{"module": "...", "version": "..."}]}.
func handleDraft(w http.ResponseWriter, r *http.Request) {
	st := llmStatus()
	if !st.Enabled {
		writeError(w, http.StatusForbidden, errors.New(st.Why))
		return
	}
	var req struct {
		Modules []struct{ Module, Version string } `json:"modules"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("the request is not valid JSON: %w", err))
		return
	}
	if len(req.Modules) == 0 || len(req.Modules) > maxDraftModules {
		writeError(w, http.StatusBadRequest, fmt.Errorf(`send {"modules": [...]} with 1 to %d modules`, maxDraftModules))
		return
	}
	key, _ := anthropicKey()
	out := make([]importer.DraftJSON, len(req.Modules))
	var wg sync.WaitGroup
	slots := make(chan struct{}, 4) // a few at a time: quick, and gentle on a small rate limit
	for i, m := range req.Modules {
		wg.Add(1)
		go func() {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			out[i] = importer.DraftModule(r.Context(), key, strings.TrimSpace(m.Module), strings.TrimSpace(m.Version))
		}()
	}
	wg.Wait()
	respondJSON(w, map[string]any{"model": st.Model, "drafts": out})
}

// handleGoMods answers POST /gomods: {"files": ["module x ...", ...]}.
func handleGoMods(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Files []string `json:"files"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("the request is not valid JSON: %w", err))
		return
	}
	if len(req.Files) > maxGoMods {
		writeError(w, http.StatusBadRequest, fmt.Errorf(`send {"files": [...]} with at most %d go.mod files as text`, maxGoMods))
		return
	}
	respondJSON(w, importer.ScanGoMods(req.Files))
}

// maxGoMods bounds one request: enough for a team's services, not a crawl.
const maxGoMods = 50
