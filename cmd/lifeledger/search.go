package main

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/rohitshukla001/lifeledger/internal/agent"
	"github.com/rohitshukla001/lifeledger/internal/tavily"
)

func (a *app) tavilyClient() (*tavily.Client, error) {
	return tavily.New(tavily.Options{APIKey: a.cfg.TavilyAPIKey, BaseURL: a.cfg.TavilyBaseURL, Logger: a.log})
}

func (a *app) searcher() agent.Searcher {
	if a.cfg.TavilyAPIKey == "" {
		return nil
	}
	c, err := a.tavilyClient()
	if err != nil {
		a.log.Warn("web search is off", "err", err)
		return nil
	}
	return c
}

func (a *app) search(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	topic := fs.String("topic", "general", "general, news, or finance")
	timeRange := fs.String("range", "", "only pages from the last day, week, month, or year")
	n := fs.Int("n", 5, "number of results (1-20)")
	site := fs.String("site", "", "comma-separated domains to search, for example incometax.gov.in")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	query := strings.Join(fs.Args(), " ")
	if strings.TrimSpace(query) == "" {
		fmt.Fprintln(a.stderr, "usage: lifeledger search [-topic T] [-range R] [-n N] [-site D] QUERY")
		return 2
	}

	client, err := a.tavilyClient()
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	var domains []string
	for _, d := range strings.Split(*site, ",") {
		if d = strings.TrimSpace(d); d != "" {
			domains = append(domains, d)
		}
	}
	res, err := client.Search(ctx, tavily.Query{Text: query, Topic: *topic, TimeRange: *timeRange, MaxResults: *n, IncludeDomains: domains})
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}

	if res.Answer != "" {
		fmt.Fprintf(a.stdout, "%s\n\n", res.Answer)
	}
	if len(res.Results) == 0 {
		fmt.Fprintln(a.stdout, "No results.")
	}
	for i, r := range res.Results {
		fmt.Fprintf(a.stdout, "[%d] %s\n    %s\n", i+1, r.Title, r.URL)
		if r.PublishedDate != "" {
			fmt.Fprintf(a.stdout, "    published %s\n", r.PublishedDate)
		}
	}
	fmt.Fprintf(a.stderr, "  [%d results, %.0f credits, %.2fs]\n", len(res.Results), res.Usage.Credits, res.ResponseTime)
	return 0
}
