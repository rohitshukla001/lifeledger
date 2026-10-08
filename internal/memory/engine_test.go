package memory

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rohitshukla001/lifeledger/internal/store"
)

var concepts = [][]string{
	{"car", "vehicle", "insurance", "policy", "motor"},
	{"electricity", "power", "bescom", "meter"},
	{"whatsapp", "message", "reminder", "notify", "ping"},
	{"passport", "visa", "travel", "document"},
}

type fakeEmbedder struct {
	calls   int
	fail    bool
	queries []string
}

func (f *fakeEmbedder) EmbeddingModel() string { return "fake-embed" }

func (f *fakeEmbedder) Embed(_ context.Context, inputs []string) ([][]float32, error) {
	f.calls++
	if f.fail {
		return nil, errors.New("token factory unavailable")
	}
	out := make([][]float32, len(inputs))
	for i, in := range inputs {
		if q, ok := strings.CutPrefix(in, queryPrefix); ok {
			f.queries = append(f.queries, q)
			in = q
		}
		vec := make([]float32, len(concepts)+1)
		for _, w := range tokenize(in) {
			dim := len(concepts)
			for d, group := range concepts {
				for _, c := range group {
					if sameStem(w, c) {
						dim = d
					}
				}
			}
			vec[dim]++
		}
		out[i] = vec
	}
	return out, nil
}

func newEngine(t *testing.T, e Embedder) (*Engine, *store.Store) {
	t.Helper()
	s, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "mem.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return New(s, e, nil), s
}

func remember(t *testing.T, e *Engine, content string) store.Memory {
	t.Helper()
	m, _, err := e.Remember(context.Background(), store.Memory{Content: content, Source: "test"})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func topContent(t *testing.T, hits []Hit) string {
	t.Helper()
	if len(hits) == 0 {
		t.Fatal("no hits")
	}
	return hits[0].Memory.Content
}

func TestRememberStoresEmbeddingAndSource(t *testing.T) {
	emb := &fakeEmbedder{}
	e, s := newEngine(t, emb)
	ctx := context.Background()

	m, created, err := e.Remember(ctx, store.Memory{Kind: store.MemoryPreference, Content: "  Prefers WhatsApp reminders  ", Source: "chat", SourceRef: "conversation:7"})
	if err != nil || !created {
		t.Fatalf("Remember: %v, created=%v", err, created)
	}
	if m.Content != "Prefers WhatsApp reminders" || m.Source != "chat" || m.SourceRef != "conversation:7" {
		t.Fatalf("unexpected memory: %+v", m)
	}
	vecs, _ := s.MemoryEmbeddings(ctx, "fake-embed")
	if _, ok := vecs[m.ID]; !ok {
		t.Fatal("embedding was not stored")
	}
}

func TestRememberSkipsDuplicates(t *testing.T) {
	emb := &fakeEmbedder{}
	e, _ := newEngine(t, emb)

	first := remember(t, e, "Car insurance renews in March.")
	again, created, err := e.Remember(context.Background(), store.Memory{Content: "car insurance renews in march"})
	if err != nil || created || again.ID != first.ID {
		t.Fatalf("duplicate was saved again: %+v, created=%v, err=%v", again, created, err)
	}
	if emb.calls != 1 {
		t.Fatalf("embed calls = %d, duplicate must not be embedded", emb.calls)
	}
}

func TestRememberRejectsEmptyContent(t *testing.T) {
	e, _ := newEngine(t, nil)
	if _, _, err := e.Remember(context.Background(), store.Memory{Content: "   "}); err == nil {
		t.Fatal("want error for empty content")
	}
}

func TestRecallFindsMeaningWithoutSharedWords(t *testing.T) {
	emb := &fakeEmbedder{}
	e, _ := newEngine(t, emb)
	remember(t, e, "Electricity provider is BESCOM")
	remember(t, e, "Car insurance with Acko renews in March")
	remember(t, e, "Passport expires in 2031")

	hits, err := e.Recall(context.Background(), "vehicle policy deadline", 3)
	if err != nil {
		t.Fatal(err)
	}
	if got := topContent(t, hits); got != "Car insurance with Acko renews in March" {
		t.Fatalf("top hit = %q", got)
	}
	if len(emb.queries) != 1 || emb.queries[0] != "vehicle policy deadline" {
		t.Fatalf("query was not embedded with the retrieval instruction: %v", emb.queries)
	}
	for i := 1; i < len(hits); i++ {
		if hits[i].Score > hits[i-1].Score {
			t.Fatalf("hits not sorted by score: %+v", hits)
		}
	}
}

func TestRecallKeywordOnlyWithoutEmbedder(t *testing.T) {
	e, _ := newEngine(t, nil)
	remember(t, e, "Electricity bills are paid from the HDFC account")
	remember(t, e, "Prefers WhatsApp reminders")

	hits, err := e.Recall(context.Background(), "which account pays the electricity bill?", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Memory.Content != "Electricity bills are paid from the HDFC account" {
		t.Fatalf("hits = %+v", hits)
	}

	if hits, _ := e.Recall(context.Background(), "favourite movie", 5); len(hits) != 0 {
		t.Fatalf("unrelated query returned %+v", hits)
	}
}

func TestRecallFallsBackWhenEmbeddingFails(t *testing.T) {
	emb := &fakeEmbedder{}
	e, _ := newEngine(t, emb)
	remember(t, e, "Electricity provider is BESCOM")

	emb.fail = true
	hits, err := e.Recall(context.Background(), "electricity provider", 5)
	if err != nil || len(hits) != 1 {
		t.Fatalf("hits = %+v, err = %v", hits, err)
	}
}

func TestRecallHandlesEmptyInputs(t *testing.T) {
	e, _ := newEngine(t, &fakeEmbedder{})
	if hits, err := e.Recall(context.Background(), "anything", 5); err != nil || hits != nil {
		t.Fatalf("empty store: %v, %v", hits, err)
	}
	remember(t, e, "Passport expires in 2031")
	if hits, err := e.Recall(context.Background(), "   ", 5); err != nil || hits != nil {
		t.Fatalf("blank query: %v, %v", hits, err)
	}
}

func TestRecallRespectsLimit(t *testing.T) {
	e, _ := newEngine(t, nil)
	for i := range 8 {
		remember(t, e, fmt.Sprintf("Electricity meter reading %d", i))
	}
	hits, err := e.Recall(context.Background(), "electricity meter", 3)
	if err != nil || len(hits) != 3 {
		t.Fatalf("got %d hits, %v; want 3", len(hits), err)
	}
}

func TestEditReembedsAndForgetRemoves(t *testing.T) {
	emb := &fakeEmbedder{}
	e, s := newEngine(t, emb)
	ctx := context.Background()

	m := remember(t, e, "Electricity provider is BESCOM")
	edited, err := e.Edit(ctx, m.ID, "Passport is kept in the locker")
	if err != nil || edited.Content != "Passport is kept in the locker" {
		t.Fatalf("Edit: %+v, %v", edited, err)
	}
	vecs, _ := s.MemoryEmbeddings(ctx, "fake-embed")
	if vecs[m.ID][3] == 0 {
		t.Fatalf("embedding not refreshed after edit: %v", vecs[m.ID])
	}
	if hits, _ := e.Recall(ctx, "travel document", 5); len(hits) != 1 {
		t.Fatalf("edited memory not found by new meaning: %+v", hits)
	}

	if err := e.Forget(ctx, m.ID); err != nil {
		t.Fatal(err)
	}
	if hits, _ := e.Recall(ctx, "travel document", 5); len(hits) != 0 {
		t.Fatalf("forgotten memory still recalled: %+v", hits)
	}
	if err := e.Forget(ctx, m.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second forget: %v", err)
	}
	if _, err := e.Edit(ctx, m.ID, "x"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("edit forgotten: %v", err)
	}
}

func TestEmbeddingFailureKeepsMemoryAndReindexRepairs(t *testing.T) {
	emb := &fakeEmbedder{fail: true}
	e, s := newEngine(t, emb)
	ctx := context.Background()

	for i := range reindexBatch + 5 {
		remember(t, e, fmt.Sprintf("Bill number %d", i))
	}
	if vecs, _ := s.MemoryEmbeddings(ctx, "fake-embed"); len(vecs) != 0 {
		t.Fatalf("no embeddings expected while the embedder fails, got %d", len(vecs))
	}

	emb.fail = false
	n, err := e.Reindex(ctx)
	if err != nil || n != reindexBatch+5 {
		t.Fatalf("Reindex = %d, %v; want %d", n, err, reindexBatch+5)
	}
	if n, err := e.Reindex(ctx); err != nil || n != 0 {
		t.Fatalf("second Reindex = %d, %v; want 0", n, err)
	}
}

func TestReindexNeedsEmbedder(t *testing.T) {
	e, _ := newEngine(t, nil)
	if _, err := e.Reindex(context.Background()); !errors.Is(err, ErrNoEmbedder) {
		t.Fatalf("err = %v, want ErrNoEmbedder", err)
	}
}

func TestKeywordScore(t *testing.T) {
	cases := []struct {
		query, content string
		want           float64
	}{
		{"electricity bill", "Electricity bills are due on the 5th", 1},
		{"when is my rent due", "Rent is due on the 1st", 1},
		{"rent insurance", "Rent is due on the 1st", 0.5},
		{"the of and", "anything", 0},
		{"pin", "pincode 560001", 0},
	}
	for _, c := range cases {
		if got := keywordScore(tokenize(c.query), c.content); got != c.want {
			t.Errorf("keywordScore(%q, %q) = %v, want %v", c.query, c.content, got, c.want)
		}
	}
}
