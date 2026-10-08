package memory

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strings"

	"github.com/rohitshukla001/lifeledger/internal/store"
)

const (
	semanticWeight = 0.75
	minScore       = 0.2
	reindexBatch   = 32
	queryPrefix    = "Instruct: Given a question about the user's life admin, retrieve the stored facts that help answer it\nQuery: "
)

var ErrNoEmbedder = errors.New("memory: no embedding model configured")

type Embedder interface {
	Embed(ctx context.Context, inputs []string) ([][]float32, error)
	EmbeddingModel() string
}

type Hit struct {
	Memory store.Memory
	Score  float64
}

type Engine struct {
	store    *store.Store
	embedder Embedder
	log      *slog.Logger
}

func New(s *store.Store, embedder Embedder, log *slog.Logger) *Engine {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Engine{store: s, embedder: embedder, log: log}
}

func (e *Engine) Remember(ctx context.Context, m store.Memory) (store.Memory, bool, error) {
	m.Content = strings.TrimSpace(m.Content)
	if m.Content == "" {
		return store.Memory{}, false, errors.New("memory: content is empty")
	}

	existing, err := e.store.ListMemories(ctx, 0)
	if err != nil {
		return store.Memory{}, false, err
	}
	key := normalize(m.Content)
	for _, old := range existing {
		if normalize(old.Content) == key {
			return old, false, nil
		}
	}

	if err := e.store.AddMemory(ctx, &m); err != nil {
		return store.Memory{}, false, err
	}
	e.embedOne(ctx, m.ID, m.Content)
	return m, true, nil
}

func (e *Engine) Edit(ctx context.Context, id int64, content string) (store.Memory, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return store.Memory{}, errors.New("memory: content is empty")
	}
	if err := e.store.UpdateMemoryContent(ctx, id, content); err != nil {
		return store.Memory{}, err
	}
	e.embedOne(ctx, id, content)
	return e.store.GetMemory(ctx, id)
}

func (e *Engine) Forget(ctx context.Context, id int64) error {
	return e.store.DeleteMemory(ctx, id)
}

func (e *Engine) List(ctx context.Context, limit int) ([]store.Memory, error) {
	return e.store.ListMemories(ctx, limit)
}

func (e *Engine) Recall(ctx context.Context, query string, limit int) ([]Hit, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 5
	}

	memories, err := e.store.ListMemories(ctx, 0)
	if err != nil || len(memories) == 0 {
		return nil, err
	}

	queryVec, vectors := e.semanticInputs(ctx, query)
	terms := tokenize(query)

	var hits []Hit
	for _, m := range memories {
		score := keywordScore(terms, m.Content)
		if vec, ok := vectors[m.ID]; ok && len(vec) == len(queryVec) {
			score = semanticWeight*dot(queryVec, vec) + (1-semanticWeight)*score
		}
		if score >= minScore {
			hits = append(hits, Hit{Memory: m, Score: score})
		}
	}

	slices.SortStableFunc(hits, func(a, b Hit) int {
		return cmp.Compare(b.Score, a.Score)
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

func (e *Engine) Reindex(ctx context.Context) (int, error) {
	if e.embedder == nil {
		return 0, ErrNoEmbedder
	}
	model := e.embedder.EmbeddingModel()
	done := 0
	for {
		batch, err := e.store.MemoriesWithoutEmbedding(ctx, model, reindexBatch)
		if err != nil || len(batch) == 0 {
			return done, err
		}
		texts := make([]string, len(batch))
		for i, m := range batch {
			texts[i] = m.Content
		}
		vecs, err := e.embedder.Embed(ctx, texts)
		if err != nil {
			return done, fmt.Errorf("memory: reindex: %w", err)
		}
		for i, m := range batch {
			if err := e.store.SetMemoryEmbedding(ctx, m.ID, model, normalized(vecs[i])); err != nil {
				return done, err
			}
			done++
		}
	}
}

func (e *Engine) embedOne(ctx context.Context, id int64, content string) {
	if e.embedder == nil {
		return
	}
	vecs, err := e.embedder.Embed(ctx, []string{content})
	if err == nil {
		err = e.store.SetMemoryEmbedding(ctx, id, e.embedder.EmbeddingModel(), normalized(vecs[0]))
	}
	if err != nil {
		e.log.Warn("memory saved without embedding; run reindex later", "memory_id", id, "err", err)
	}
}

func (e *Engine) semanticInputs(ctx context.Context, query string) ([]float32, map[int64][]float32) {
	if e.embedder == nil {
		return nil, nil
	}
	vecs, err := e.embedder.Embed(ctx, []string{queryPrefix + query})
	if err != nil {
		e.log.Warn("recall falls back to keyword match", "err", err)
		return nil, nil
	}
	stored, err := e.store.MemoryEmbeddings(ctx, e.embedder.EmbeddingModel())
	if err != nil {
		e.log.Warn("recall falls back to keyword match", "err", err)
		return nil, nil
	}
	return normalized(vecs[0]), stored
}

func normalized(v []float32) []float32 {
	var sum float64
	for _, f := range v {
		sum += float64(f) * float64(f)
	}
	if sum == 0 {
		return v
	}
	n := float32(1 / math.Sqrt(sum))
	out := make([]float32, len(v))
	for i, f := range v {
		out[i] = f * n
	}
	return out
}

func dot(a, b []float32) float64 {
	var sum float64
	for i := range a {
		sum += float64(a[i]) * float64(b[i])
	}
	return sum
}
