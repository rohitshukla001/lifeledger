package money

import "testing"

func TestParse(t *testing.T) {
	ok := map[string]int64{"0": 0, "12": 1200, "12.5": 1250, "1,234.56": 123456, ".75": 75, " 2 340 ": 234000, "99.": 9900}
	for in, want := range ok {
		if got, err := Parse(in); err != nil || got != want {
			t.Errorf("Parse(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "abc", "-1", "1.234", "1e5", "92233720368547758.08"} {
		if _, err := Parse(in); err == nil {
			t.Errorf("Parse(%q): want error", in)
		}
	}
	if got := Format(123405); got != "1234.05" {
		t.Errorf("Format = %q", got)
	}
}
