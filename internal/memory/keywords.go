package memory

import (
	"strings"
	"unicode"
)

var stopwords = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "at": true, "be": true, "by": true, "do": true,
	"for": true, "from": true, "have": true, "how": true, "i": true, "in": true, "is": true, "it": true,
	"me": true, "my": true, "of": true, "on": true, "or": true, "the": true, "to": true, "was": true,
	"what": true, "when": true, "where": true, "which": true, "who": true, "with": true, "you": true,
	"your": true, "does": true, "did": true, "can": true, "about": true,
}

func tokenize(s string) []string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	seen := map[string]bool{}
	var out []string
	for _, f := range fields {
		if len(f) < 2 || stopwords[f] || seen[f] {
			continue
		}
		seen[f] = true
		out = append(out, f)
	}
	return out
}

func normalize(s string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}), " ")
}

func keywordScore(terms []string, content string) float64 {
	if len(terms) == 0 {
		return 0
	}
	words := tokenize(content)
	matched := 0
	for _, t := range terms {
		for _, w := range words {
			if sameStem(t, w) {
				matched++
				break
			}
		}
	}
	return float64(matched) / float64(len(terms))
}

func sameStem(a, b string) bool {
	if a == b {
		return true
	}
	if len(a) < 4 || len(b) < 4 {
		return false
	}
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}
