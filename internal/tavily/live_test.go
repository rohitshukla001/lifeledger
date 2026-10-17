package tavily_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rohitshukla001/lifeledger/internal/config"
	"github.com/rohitshukla001/lifeledger/internal/tavily"
)

func TestLiveSearch(t *testing.T) {
	if os.Getenv("LIFELEDGER_LIVE_TEST") != "1" {
		t.Skip("set LIFELEDGER_LIVE_TEST=1 to call the real Tavily API")
	}
	cfg, err := config.Load(filepath.Join("..", "..", ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TavilyAPIKey == "" {
		t.Skip("TAVILY_API_KEY is not set")
	}
	c, err := tavily.New(tavily.Options{APIKey: cfg.TavilyAPIKey, BaseURL: cfg.TavilyBaseURL})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	res, err := c.Search(ctx, tavily.Query{Text: "Nebius Token Factory NVIDIA Nemotron models", MaxResults: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Results) == 0 {
		t.Fatal("no results")
	}
	for _, r := range res.Results {
		t.Logf("%.2f %s %s", r.Score, r.Title, r.URL)
		if !strings.HasPrefix(r.URL, "http") || r.Title == "" {
			t.Errorf("result without a usable citation: %+v", r)
		}
	}
	t.Logf("answer: %s (%.0f credits, %.2fs)", res.Answer, res.Usage.Credits, res.ResponseTime)
}
