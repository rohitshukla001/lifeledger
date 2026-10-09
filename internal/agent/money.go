package agent

import (
	"fmt"
	"strconv"
	"strings"
)

func parseAmount(s string) (int64, error) {
	clean := strings.NewReplacer(",", "", " ", "", "_", "").Replace(strings.TrimSpace(s))
	whole, frac, hasFrac := strings.Cut(clean, ".")
	if whole == "" && !hasFrac {
		return 0, fmt.Errorf("amount %q is empty", s)
	}
	if len(frac) > 2 {
		return 0, fmt.Errorf("amount %q has more than 2 decimal places", s)
	}
	frac += strings.Repeat("0", 2-len(frac))
	if whole == "" {
		whole = "0"
	}
	units, err := strconv.ParseUint(whole, 10, 63)
	if err != nil {
		return 0, fmt.Errorf("amount %q is not a positive number", s)
	}
	cents, err := strconv.ParseUint(frac, 10, 8)
	if err != nil {
		return 0, fmt.Errorf("amount %q is not a positive number", s)
	}
	if units > (1<<63-1-cents)/100 {
		return 0, fmt.Errorf("amount %q is too large", s)
	}
	return int64(units*100 + cents), nil
}

func formatAmount(minor int64) string {
	return fmt.Sprintf("%d.%02d", minor/100, minor%100)
}
