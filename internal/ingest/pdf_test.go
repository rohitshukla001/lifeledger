package ingest

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func testPDF(lines ...string) []byte {
	var content strings.Builder
	content.WriteString("BT /F1 12 Tf 72 720 Td 16 TL\n")
	for _, l := range lines {
		fmt.Fprintf(&content, "(%s) Tj T*\n", l)
	}
	content.WriteString("ET")

	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", content.Len(), content.String()),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
	}

	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for i, obj := range objects {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return b.Bytes()
}

func TestPDFTextExtraction(t *testing.T) {
	text, err := pdfText(testPDF("Airtel Broadband invoice", "Amount due: Rs 999.00", "Pay by 15 Nov 2026"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Airtel Broadband invoice", "999.00", "15 Nov 2026"} {
		if !strings.Contains(text, want) {
			t.Errorf("text %q does not contain %q", text, want)
		}
	}
}

func TestPDFRejectsBadInput(t *testing.T) {
	cases := map[string][]byte{
		"not a pdf": []byte("hello"),
		"truncated": []byte("%PDF-1.4\n1 0 obj"),
		"no text":   testPDF(),
		"too large": append([]byte("%PDF-"), make([]byte, MaxPDFBytes)...),
	}
	for name, data := range cases {
		if _, err := pdfText(data); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	if _, err := pdfText(testPDF()); !errors.Is(err, ErrEmpty) {
		t.Errorf("empty PDF: err = %v, want ErrEmpty", err)
	}
}
