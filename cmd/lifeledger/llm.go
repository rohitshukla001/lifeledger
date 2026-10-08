package main

import (
	"context"
	"flag"
	"fmt"
	"slices"
	"strings"

	"github.com/rohitshukla001/lifeledger/internal/llm"
)

func (a *app) llmClient() (*llm.Client, error) {
	return llm.New(llm.Options{
		BaseURL: a.cfg.NebiusBaseURL,
		APIKey:  a.cfg.NebiusAPIKey,
		Models: map[llm.Tier]string{
			llm.Nano:  a.cfg.ModelNano,
			llm.Super: a.cfg.ModelSuper,
			llm.Ultra: a.cfg.ModelUltra,
		},
		EmbedModel:     a.cfg.ModelEmbed,
		DailyBudgetUSD: a.cfg.DailyBudgetUSD,
		Logger:         a.log,
	})
}

func (a *app) models(ctx context.Context) int {
	client, err := a.llmClient()
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	ids, err := client.ListModels(ctx)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}

	type role struct{ name, id string }
	roles := []role{
		{"nano", client.Model(llm.Nano)},
		{"super", client.Model(llm.Super)},
		{"ultra", client.Model(llm.Ultra)},
		{"embed", client.EmbeddingModel()},
	}
	for _, id := range ids {
		i := slices.IndexFunc(roles, func(r role) bool { return r.id == id })
		if i >= 0 {
			fmt.Fprintf(a.stdout, "* %s (%s)\n", id, roles[i].name)
			continue
		}
		fmt.Fprintf(a.stdout, "  %s\n", id)
	}

	missing := 0
	for _, r := range roles {
		if !slices.Contains(ids, r.id) {
			fmt.Fprintf(a.stderr, "configured %s model %q is not in the Token Factory catalog\n", r.name, r.id)
			missing++
		}
	}
	if missing > 0 {
		return 1
	}
	return 0
}

func (a *app) ask(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("ask", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	tierFlag := fs.String("tier", string(llm.Super), "model tier: nano, super, or ultra")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	tier, ok := llm.ParseTier(*tierFlag)
	if !ok {
		fmt.Fprintf(a.stderr, "unknown tier %q\n", *tierFlag)
		return 2
	}
	prompt := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if prompt == "" {
		fmt.Fprintln(a.stderr, "usage: lifeledger ask [-tier nano|super|ultra] PROMPT")
		return 2
	}

	client, err := a.llmClient()
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	resp, err := client.Chat(ctx, llm.Request{Tier: tier, Messages: []llm.Message{llm.User(prompt)}})
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}

	fmt.Fprintln(a.stdout, resp.Message.Content)
	fmt.Fprintf(a.stderr, "\nmodel=%s tokens=%d/%d cost=$%.6f\n",
		resp.Model, resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.CostUSD)
	return 0
}
