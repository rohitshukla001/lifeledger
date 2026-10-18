package api

import (
	"net/http"
	"time"

	"github.com/rohitshukla001/lifeledger/internal/ledger"
	"github.com/rohitshukla001/lifeledger/internal/store"
)

func (a *API) listObligations(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	n, err := limit(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	f := ledger.Filter{Status: q.Get("status"), Category: q.Get("category"), Limit: n}
	if q.Has("due_within_days") {
		days, err := queryInt(r, "due_within_days", 0, 0, 3660)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		f.DueWithinDays = &days
	}
	today := a.today()
	filter, err := f.Store(today)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	found, err := a.store.ListObligations(r.Context(), filter)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Today string           `json:"today"`
		Items []obligationJSON `json:"items"`
	}{today.Format(time.DateOnly), mapSlice(found, a.obligationView)})
}

func (a *API) createObligation(w http.ResponseWriter, r *http.Request) {
	var in ledger.Input
	if err := decode(w, r, &in); err != nil {
		a.fail(w, r, err)
		return
	}
	o := store.Obligation{Source: "user"}
	if err := in.ApplyTo(&o, a.currency); err != nil {
		a.fail(w, r, err)
		return
	}
	if err := a.store.CreateObligation(r.Context(), &o); err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, a.obligationView(o))
}

func (a *API) getObligation(w http.ResponseWriter, r *http.Request) {
	o, err := a.loadObligation(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, a.obligationView(o))
}

func (a *API) updateObligation(w http.ResponseWriter, r *http.Request) {
	o, err := a.loadObligation(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	var in ledger.Input
	if err := decode(w, r, &in); err != nil {
		a.fail(w, r, err)
		return
	}
	if err := in.ApplyTo(&o, a.currency); err != nil {
		a.fail(w, r, err)
		return
	}
	if o.Status == store.StatusSnoozed && o.SnoozedUntil == nil {
		a.fail(w, r, ledger.Invalidf("snoozed_until is required when status is snoozed"))
		return
	}
	if err := a.store.UpdateObligation(r.Context(), &o); err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, a.obligationView(o))
}

func (a *API) deleteObligation(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if err := a.store.DeleteObligation(r.Context(), id); err != nil {
		a.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) loadObligation(r *http.Request) (store.Obligation, error) {
	id, err := pathID(r)
	if err != nil {
		return store.Obligation{}, err
	}
	return a.store.GetObligation(r.Context(), id)
}
