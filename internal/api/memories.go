package api

import (
	"net/http"
	"slices"
	"strings"

	"github.com/rohitshukla001/lifeledger/internal/ledger"
	"github.com/rohitshukla001/lifeledger/internal/store"
)

var memoryKinds = []store.MemoryKind{store.MemoryFact, store.MemoryPreference, store.MemoryNote}

func (a *API) listMemories(w http.ResponseWriter, r *http.Request) {
	n, err := limit(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	found, err := a.memory.List(r.Context(), n)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list(mapSlice(found, memoryView)))
}

func (a *API) createMemory(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Content string           `json:"content"`
		Kind    store.MemoryKind `json:"kind"`
	}
	if err := decode(w, r, &in); err != nil {
		a.fail(w, r, err)
		return
	}
	if in.Kind == "" {
		in.Kind = store.MemoryFact
	}
	if !slices.Contains(memoryKinds, in.Kind) {
		a.fail(w, r, ledger.Invalidf("kind must be fact, preference, or note"))
		return
	}
	m, created, err := a.memory.Remember(r.Context(), store.Memory{Kind: in.Kind, Content: in.Content, Source: "user"})
	if err != nil {
		a.fail(w, r, err)
		return
	}
	status := http.StatusCreated
	if !created {
		status = http.StatusOK
	}
	writeJSON(w, status, memoryView(m))
}

func (a *API) recallMemories(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		a.fail(w, r, ledger.Invalidf("q is required"))
		return
	}
	n, err := queryInt(r, "limit", 5, 1, 50)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	hits, err := a.memory.Recall(r.Context(), query, n)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list(mapSlice(hits, hitView)))
}

func (a *API) updateMemory(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	var in struct {
		Content string `json:"content"`
	}
	if err := decode(w, r, &in); err != nil {
		a.fail(w, r, err)
		return
	}
	m, err := a.memory.Edit(r.Context(), id, in.Content)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, memoryView(m))
}

func (a *API) deleteMemory(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if err := a.memory.Forget(r.Context(), id); err != nil {
		a.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
