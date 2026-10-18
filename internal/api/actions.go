package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"

	"github.com/rohitshukla001/lifeledger/internal/actions"
	"github.com/rohitshukla001/lifeledger/internal/ledger"
	"github.com/rohitshukla001/lifeledger/internal/store"
)

var actionStatuses = []store.ActionStatus{store.ActionPending, store.ActionApproved, store.ActionRejected, store.ActionDone, store.ActionFailed}

func (a *API) listActions(w http.ResponseWriter, r *http.Request) {
	n, err := limit(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	status := store.ActionStatus(r.URL.Query().Get("status"))
	if status != "" && !slices.Contains(actionStatuses, status) {
		a.fail(w, r, ledger.Invalidf("unknown status %q", status))
		return
	}
	found, err := a.store.ListActions(r.Context(), status, n)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list(mapSlice(found, actionView)))
}

func (a *API) requestAction(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Kind         string          `json:"kind"`
		ObligationID *int64          `json:"obligation_id"`
		Payload      json.RawMessage `json:"payload"`
	}
	if err := decode(w, r, &in); err != nil {
		a.fail(w, r, err)
		return
	}
	act, err := a.actions.Request(r.Context(), actions.Request{
		Kind:         in.Kind,
		ObligationID: in.ObligationID,
		Payload:      in.Payload,
		RequestedBy:  actions.ActorUser,
		SourceRef:    "api",
	})
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, actionView(act))
}

func (a *API) getAction(w http.ResponseWriter, r *http.Request) {
	act, err := a.loadAction(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, actionView(act))
}

func (a *API) approveAction(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	act, err := a.actions.Approve(r.Context(), id, actions.ActorUser)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, actionView(act))
}

func (a *API) rejectAction(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if err := a.actions.Reject(r.Context(), id, actions.ActorUser); err != nil {
		a.fail(w, r, err)
		return
	}
	act, err := a.actions.Get(r.Context(), id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, actionView(act))
}

func (a *API) actionCalendar(w http.ResponseWriter, r *http.Request) {
	act, err := a.loadAction(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if act.Kind != actions.KindCalendarEvent || act.Status != store.ActionDone {
		a.fail(w, r, ledger.Invalidf("action %d is not a finished calendar action", act.ID))
		return
	}
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="lifeledger-%d.ics"`, act.ID))
	_, _ = w.Write([]byte(act.Result))
}

func (a *API) listAudit(w http.ResponseWriter, r *http.Request) {
	n, err := limit(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	entries, err := a.store.ListAudit(r.Context(), n)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list(mapSlice(entries, func(e store.AuditEntry) auditJSON {
		return auditJSON{ID: e.ID, Actor: e.Actor, Event: e.Event, Subject: e.Subject, Detail: e.Detail, CreatedAt: e.CreatedAt}
	})))
}

func (a *API) loadAction(r *http.Request) (store.Action, error) {
	id, err := pathID(r)
	if err != nil {
		return store.Action{}, err
	}
	return a.actions.Get(r.Context(), id)
}
