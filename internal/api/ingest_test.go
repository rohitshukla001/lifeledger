package api

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/rohitshukla001/lifeledger/internal/ingest"
	"github.com/rohitshukla001/lifeledger/internal/llm"
)

const airtelJSON = `{"summary": "Airtel broadband bill", "obligations": [
	{"title": "Airtel broadband", "category": "bill", "amount": "999", "currency": "INR", "due_on": "2026-11-15", "recurrence": "monthly", "notes": "", "evidence": "Pay by 15 Nov", "confidence": 0.9},
	{"title": "Airtel router warranty", "category": "warranty", "amount": null, "currency": null, "due_on": "2027-06-01", "recurrence": "none", "notes": "", "evidence": "Warranty till June 2027", "confidence": 0.7}
]}`

func multipartBody(t *testing.T, filename string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	fw.Write(content)
	mw.Close()
	return &buf, mw.FormDataContentType()
}

func TestIngestAndProposals(t *testing.T) {
	f := newFixture(t)
	f.extract.push(say(airtelJSON))

	res := f.do(http.MethodPost, "/api/v1/ingest", map[string]string{"name": "airtel.txt", "text": "Airtel bill: pay 999 by 15 Nov 2026."}, http.StatusCreated)
	props := res["proposals"].([]any)
	if res["source_kind"] != "text" || res["source_name"] != "airtel.txt" || res["reused"] != false || len(props) != 2 {
		t.Fatalf("ingest = %v", res)
	}
	if p := props[0].(map[string]any); p["amount"] != "999.00" || p["due_on"] != "2026-11-15" || p["status"] != "pending" {
		t.Fatalf("proposal = %v", p)
	}
	if again := f.do(http.MethodPost, "/api/v1/ingest", map[string]string{"text": "Airtel bill: pay 999 by 15 Nov 2026."}, http.StatusOK); again["reused"] != true {
		t.Fatalf("second ingest = %v", again)
	}

	if pending := f.items("/api/v1/proposals"); len(pending) != 2 {
		t.Fatalf("pending = %v", pending)
	}
	o := f.do(http.MethodPost, "/api/v1/proposals/1/accept", nil, http.StatusCreated)
	if o["title"] != "Airtel broadband" || o["source"] != "ingest" {
		t.Fatalf("accepted obligation = %v", o)
	}
	f.wantError(http.MethodPost, "/api/v1/proposals/1/accept", nil, http.StatusConflict, "already_decided", "already")
	f.do(http.MethodPost, "/api/v1/proposals/2/reject", nil, http.StatusNoContent)
	f.wantError(http.MethodPost, "/api/v1/proposals/9/reject", nil, http.StatusNotFound, "not_found", "not found")
	if all := f.items("/api/v1/proposals?status=all"); len(all) != 2 {
		t.Fatalf("all = %v", all)
	}
	if accepted := f.items("/api/v1/proposals?status=accepted"); len(accepted) != 1 || accepted[0]["obligation_id"] != float64(1) {
		t.Fatalf("accepted = %v", accepted)
	}
	f.wantError(http.MethodGet, "/api/v1/proposals?status=lost", nil, http.StatusBadRequest, "invalid_request", "unknown status")
}

func TestIngestUploads(t *testing.T) {
	f := newFixture(t)
	f.extract.push(say(airtelJSON))

	body, ctype := multipartBody(t, "airtel-bill.txt", []byte("Airtel bill: pay 999 by 15 Nov 2026."))
	rec := f.request(http.MethodPost, "/api/v1/ingest", body.Bytes(), "Content-Type", ctype)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"source_name":"airtel-bill.txt"`) {
		t.Fatalf("text upload: %d %s", rec.Code, rec.Body.String())
	}

	body, ctype = multipartBody(t, "broken.pdf", []byte("%PDF-1.4 this is not a real PDF"))
	rec = f.request(http.MethodPost, "/api/v1/ingest", body.Bytes(), "Content-Type", ctype)
	if code, msg := errorCode(t, rec); rec.Code != http.StatusBadRequest || code != "invalid_request" || !strings.Contains(msg, "PDF") {
		t.Fatalf("broken PDF: %d %s %s", rec.Code, code, msg)
	}

	body, ctype = multipartBody(t, "photo.jpg", []byte{0xff, 0xd8, 0xff, 0xe0, 0x00})
	rec = f.request(http.MethodPost, "/api/v1/ingest", body.Bytes(), "Content-Type", ctype)
	if code, msg := errorCode(t, rec); rec.Code != http.StatusBadRequest || !strings.Contains(msg, "PDF or UTF-8 text") {
		t.Fatalf("binary upload: %d %s %s", rec.Code, code, msg)
	}

	rec = f.request(http.MethodPost, "/api/v1/ingest", []byte("--x--"), "Content-Type", "multipart/form-data; boundary=x")
	if code, msg := errorCode(t, rec); rec.Code != http.StatusBadRequest || !strings.Contains(msg, "file field") {
		t.Fatalf("form without file: %d %s %s", rec.Code, code, msg)
	}

	f.wantError(http.MethodPost, "/api/v1/ingest", map[string]string{"text": "  "}, http.StatusBadRequest, "invalid_request", "no text")
}

func TestIngestNeedsAModelForNewDocuments(t *testing.T) {
	f := newFixture(t)
	f.opts.Ingest = ingest.New(ingest.Options{Store: f.store, Currency: "INR"})
	f.rebuild()
	f.wantError(http.MethodPost, "/api/v1/ingest", map[string]string{"text": "Pay 100 by Friday"}, http.StatusServiceUnavailable, "not_configured", "NEBIUS_API_KEY")

	f.opts.Ingest = ingest.New(ingest.Options{LLM: f.extract, Store: f.store, Currency: "INR"})
	f.rebuild()
	f.extract.push(fail(&llm.APIError{Status: 500, Message: "boom"}))
	f.wantError(http.MethodPost, "/api/v1/ingest", map[string]string{"text": "Pay 100 by Friday"}, http.StatusBadGateway, "model_error", "boom")
}
