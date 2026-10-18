package api

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/rohitshukla001/lifeledger/internal/ingest"
	"github.com/rohitshukla001/lifeledger/internal/ledger"
	"github.com/rohitshukla001/lifeledger/internal/store"
)

const maxUploadBytes = ingest.MaxPDFBytes + 1<<20

func (a *API) ingestDocument(w http.ResponseWriter, r *http.Request) {
	name, data, err := readDocument(w, r)
	if err != nil {
		a.fail(w, r, err)
		return
	}

	var res *ingest.Result
	if bytes.HasPrefix(data, []byte("%PDF-")) {
		res, err = a.ingest.PDF(r.Context(), name, data)
	} else if !utf8.Valid(data) {
		err = ledger.Invalidf("the document must be a PDF or UTF-8 text")
	} else {
		res, err = a.ingest.Text(r.Context(), name, string(data))
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	status := http.StatusCreated
	if res.Reused {
		status = http.StatusOK
	}
	writeJSON(w, status, ingestionView(res))
}

func readDocument(w http.ResponseWriter, r *http.Request) (string, []byte, error) {
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaType != "multipart/form-data" {
		var in struct {
			Name string `json:"name"`
			Text string `json:"text"`
		}
		if err := decode(w, r, &in); err != nil {
			return "", nil, err
		}
		if strings.TrimSpace(in.Name) == "" {
			in.Name = "pasted text"
		}
		return strings.TrimSpace(in.Name), []byte(in.Text), nil
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	file, header, err := r.FormFile("file")
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return "", nil, err
		}
		return "", nil, ledger.Invalidf("the form needs a file field: %v", err)
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return "", nil, fmt.Errorf("read upload: %w", err)
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = filepath.Base(header.Filename)
	}
	return name, data, nil
}

func (a *API) listProposals(w http.ResponseWriter, r *http.Request) {
	f := store.ProposalFilter{Status: store.ProposalPending}
	switch status := r.URL.Query().Get("status"); status {
	case "", "pending":
	case "all":
		f.Status = ""
	case string(store.ProposalAccepted), string(store.ProposalRejected):
		f.Status = store.ProposalStatus(status)
	default:
		a.fail(w, r, ledger.Invalidf("unknown status %q", status))
		return
	}
	found, err := a.store.ListProposals(r.Context(), f)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list(mapSlice(found, proposalView)))
}

func (a *API) acceptProposal(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	o, err := a.ingest.Accept(r.Context(), id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, a.obligationView(o))
}

func (a *API) rejectProposal(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if err := a.ingest.Reject(r.Context(), id); err != nil {
		a.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
