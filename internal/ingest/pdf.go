package ingest

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ledongthuc/pdf"
)

const MaxPDFBytes = 10 << 20

func pdfText(data []byte) (text string, err error) {
	if len(data) > MaxPDFBytes {
		return "", fmt.Errorf("ingest: PDF is larger than %d MB", MaxPDFBytes>>20)
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		return "", errors.New("ingest: the file is not a PDF")
	}

	defer func() {
		if r := recover(); r != nil {
			text, err = "", fmt.Errorf("ingest: cannot read PDF: %v", r)
		}
	}()

	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("ingest: cannot read PDF: %w", err)
	}
	plain, err := r.GetPlainText()
	if err != nil {
		return "", fmt.Errorf("ingest: cannot read PDF text: %w", err)
	}
	b, err := io.ReadAll(io.LimitReader(plain, 4*maxChars))
	if err != nil {
		return "", fmt.Errorf("ingest: cannot read PDF text: %w", err)
	}
	if strings.TrimSpace(string(b)) == "" {
		return "", fmt.Errorf("%w: the PDF may be a scanned image", ErrEmpty)
	}
	return string(b), nil
}
