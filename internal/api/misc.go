package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rohitshukla001/lifeledger/internal/store"
	"github.com/rohitshukla001/lifeledger/internal/tavily"
)

func (a *API) overview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	today := a.today()
	active, err := a.store.ListObligations(ctx, store.ObligationFilter{Statuses: []store.Status{store.StatusOpen, store.StatusSnoozed}})
	if err != nil {
		a.fail(w, r, err)
		return
	}
	pending, err := a.store.ListActions(ctx, store.ActionPending, 0)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	proposals, err := a.store.ListProposals(ctx, store.ProposalFilter{Status: store.ProposalPending})
	if err != nil {
		a.fail(w, r, err)
		return
	}

	out := struct {
		Today            string           `json:"today"`
		Overdue          int              `json:"overdue"`
		DueToday         int              `json:"due_today"`
		DueNext7Days     int              `json:"due_next_7_days"`
		Snoozed          int              `json:"snoozed"`
		WithoutDueDate   int              `json:"without_due_date"`
		PendingActions   int              `json:"pending_actions"`
		PendingProposals int              `json:"pending_proposals"`
		Upcoming         []obligationJSON `json:"upcoming"`
	}{
		Today:            today.Format(time.DateOnly),
		PendingActions:   len(pending),
		PendingProposals: len(proposals),
		Upcoming:         []obligationJSON{},
	}
	week := today.AddDate(0, 0, 7)
	for _, o := range active {
		switch {
		case o.Status == store.StatusSnoozed:
			out.Snoozed++
			continue
		case o.DueOn == nil:
			out.WithoutDueDate++
			continue
		case o.DueOn.Before(today):
			out.Overdue++
		case o.DueOn.Equal(today):
			out.DueToday++
		case !o.DueOn.After(week):
			out.DueNext7Days++
		}
		if len(out.Upcoming) < 5 {
			out.Upcoming = append(out.Upcoming, a.obligationView(o))
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) listNotifications(w http.ResponseWriter, r *http.Request) {
	n, err := limit(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	found, err := a.store.ListNotifications(r.Context(), n)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list(mapSlice(found, notificationView)))
}

func (a *API) runSweep(w http.ResponseWriter, r *http.Request) {
	if a.sweeper == nil {
		a.fail(w, r, fmt.Errorf("%w: the sweep is off", errUnavailable))
		return
	}
	var in struct {
		Review bool `json:"review"`
	}
	if r.ContentLength != 0 {
		if err := decode(w, r, &in); err != nil {
			a.fail(w, r, err)
			return
		}
	}

	type reportJSON struct {
		Woken     int               `json:"woken"`
		Reminders int               `json:"reminders"`
		Review    bool              `json:"scheduled_review"`
		Delivered int               `json:"delivered"`
		Failed    int               `json:"failed"`
		Written   *notificationJSON `json:"review,omitempty"`
	}
	var out reportJSON
	if in.Review {
		n, err := a.sweeper.Review(r.Context())
		if err != nil {
			a.fail(w, r, err)
			return
		}
		v := notificationView(n)
		out.Written = &v
	}
	rep, err := a.sweeper.RunOnce(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	out.Woken, out.Reminders, out.Review, out.Delivered, out.Failed = rep.Woken, rep.Reminders, rep.Review, rep.Delivered, rep.Failed
	writeJSON(w, http.StatusOK, out)
}

func (a *API) webSearch(w http.ResponseWriter, r *http.Request) {
	if a.search == nil {
		a.fail(w, r, fmt.Errorf("%w: web search needs TAVILY_API_KEY", errUnavailable))
		return
	}
	q := r.URL.Query()
	n, err := queryInt(r, "max_results", 5, 1, tavily.MaxResults)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	var domains []string
	for _, d := range strings.Split(q.Get("domains"), ",") {
		if d = strings.TrimSpace(d); d != "" {
			domains = append(domains, d)
		}
	}
	res, err := a.search.Search(r.Context(), tavily.Query{
		Text: q.Get("q"), Topic: q.Get("topic"), TimeRange: q.Get("time_range"), MaxResults: n, IncludeDomains: domains,
	})
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
