package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

var ErrOutOfCredits = errors.New("llm: Token Factory account is out of credits")

type APIError struct {
	Status     int
	Message    string
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	return fmt.Sprintf("llm: token factory returned %d: %s", e.Status, e.Message)
}

func (e *APIError) Is(target error) bool {
	return target == ErrOutOfCredits && e.Status == http.StatusPaymentRequired
}

type transportError struct{ err error }

func (e *transportError) Error() string { return "llm: " + e.err.Error() }
func (e *transportError) Unwrap() error { return e.err }

var fallbackTier = map[Tier]Tier{Ultra: Super, Super: Nano}

type Options struct {
	BaseURL        string
	APIKey         string
	Models         map[Tier]string
	DailyBudgetUSD float64
	HTTPClient     *http.Client
	Logger         *slog.Logger
}

type Client struct {
	baseURL     string
	apiKey      string
	models      map[Tier]string
	budget      *Budget
	http        *http.Client
	log         *slog.Logger
	maxAttempts int
	sleep       func(context.Context, time.Duration) error
}

func New(o Options) (*Client, error) {
	if o.APIKey == "" {
		return nil, errors.New("llm: NEBIUS_API_KEY is not set")
	}
	u, err := url.Parse(o.BaseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("llm: invalid base URL %q", o.BaseURL)
	}
	for _, t := range []Tier{Nano, Super, Ultra} {
		if o.Models[t] == "" {
			return nil, fmt.Errorf("llm: no model configured for tier %q", t)
		}
	}

	c := &Client{
		baseURL:     strings.TrimSuffix(o.BaseURL, "/") + "/",
		apiKey:      o.APIKey,
		models:      o.Models,
		budget:      NewBudget(o.DailyBudgetUSD),
		http:        o.HTTPClient,
		log:         o.Logger,
		maxAttempts: 3,
		sleep:       sleepCtx,
	}
	if c.http == nil {
		c.http = &http.Client{Timeout: 3 * time.Minute}
	}
	if c.log == nil {
		c.log = slog.New(slog.DiscardHandler)
	}
	return c, nil
}

func (c *Client) Model(t Tier) string { return c.models[t] }
func (c *Client) Budget() *Budget     { return c.budget }

func (c *Client) Chat(ctx context.Context, req Request) (*Response, error) {
	tier := req.Tier
	if tier == "" {
		tier = Super
	}
	for {
		resp, err := c.complete(ctx, c.models[tier], req)
		if err == nil {
			return resp, nil
		}
		next, ok := fallbackTier[tier]
		if !ok || !retryable(err) {
			return nil, err
		}
		c.log.Warn("model unavailable, falling back", "from", c.models[tier], "to", c.models[next], "err", err)
		tier = next
	}
}

func (c *Client) ListModels(ctx context.Context) ([]string, error) {
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "models", nil, &out); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(out.Data))
	for _, m := range out.Data {
		ids = append(ids, m.ID)
	}
	slices.Sort(ids)
	return ids, nil
}

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Tools       []Tool    `json:"tools,omitempty"`
	Temperature *float64  `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
}

type chatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message      Message `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
	Usage Usage `json:"usage"`
}

func (c *Client) complete(ctx context.Context, model string, req Request) (*Response, error) {
	if err := c.budget.check(); err != nil {
		return nil, err
	}
	body, err := json.Marshal(chatRequest{
		Model:       model,
		Messages:    req.Messages,
		Tools:       req.Tools,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	})
	if err != nil {
		return nil, fmt.Errorf("llm: encode request: %w", err)
	}

	var out chatResponse
	if err := c.do(ctx, http.MethodPost, "chat/completions", body, &out); err != nil {
		return nil, err
	}
	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("llm: %s returned no choices", model)
	}

	cost := priceFor(model).cost(out.Usage)
	c.budget.add(cost)
	return &Response{
		Model:        model,
		Message:      out.Choices[0].Message,
		FinishReason: out.Choices[0].FinishReason,
		Usage:        out.Usage,
		CostUSD:      cost,
	}, nil
}

func (c *Client) do(ctx context.Context, method, path string, body []byte, out any) error {
	for attempt := 1; ; attempt++ {
		err := c.send(ctx, method, path, body, out)
		if err == nil || attempt == c.maxAttempts || !retryable(err) {
			return err
		}
		c.log.Debug("retrying token factory call", "path", path, "attempt", attempt, "err", err)
		if err := c.sleep(ctx, backoff(attempt, err)); err != nil {
			return err
		}
	}
}

func (c *Client) send(ctx context.Context, method, path string, body []byte, out any) error {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &transportError{err}
	}
	defer res.Body.Close()

	if res.StatusCode >= 300 {
		return readAPIError(res)
	}
	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		return fmt.Errorf("llm: decode %s response: %w", path, err)
	}
	return nil
}

func readAPIError(res *http.Response) *APIError {
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
	e := &APIError{Status: res.StatusCode, Message: strings.TrimSpace(string(raw))}

	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Detail  any    `json:"detail"`
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &body) == nil {
		switch {
		case body.Error.Message != "":
			e.Message = body.Error.Message
		case body.Message != "":
			e.Message = body.Message
		case body.Detail != nil:
			e.Message = fmt.Sprint(body.Detail)
		}
	}
	if e.Message == "" {
		e.Message = http.StatusText(res.StatusCode)
	}
	if secs, err := strconv.Atoi(res.Header.Get("Retry-After")); err == nil && secs > 0 {
		e.RetryAfter = time.Duration(secs) * time.Second
	}
	return e
}

func retryable(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Status == http.StatusTooManyRequests || apiErr.Status >= 500
	}
	var te *transportError
	return errors.As(err, &te)
}

func backoff(attempt int, err error) time.Duration {
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.RetryAfter > 0 {
		return min(apiErr.RetryAfter, 30*time.Second)
	}
	base := 500 * time.Millisecond << (attempt - 1)
	return base + rand.N(base/2)
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
