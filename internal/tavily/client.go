package tavily

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
	"unicode/utf8"
)

const (
	DefaultBaseURL = "https://api.tavily.com"
	MaxQueryLen    = 400
	MaxResults     = 20
)

var (
	ErrUnauthorized  = errors.New("tavily: API key is missing or invalid")
	ErrQuotaExceeded = errors.New("tavily: plan or usage limit exceeded")
)

var (
	Topics     = []string{"general", "news", "finance"}
	TimeRanges = []string{"day", "week", "month", "year"}
)

type APIError struct {
	Status     int
	Message    string
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	return fmt.Sprintf("tavily: API returned %d: %s", e.Status, e.Message)
}

func (e *APIError) Is(target error) bool {
	switch target {
	case ErrUnauthorized:
		return e.Status == http.StatusUnauthorized
	case ErrQuotaExceeded:
		return e.Status == 432 || e.Status == 433
	}
	return false
}

type transportError struct{ err error }

func (e *transportError) Error() string { return "tavily: " + e.err.Error() }
func (e *transportError) Unwrap() error { return e.err }

type Options struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
	Logger     *slog.Logger
}

type Client struct {
	apiKey      string
	endpoint    string
	http        *http.Client
	log         *slog.Logger
	maxAttempts int
	sleep       func(context.Context, time.Duration) error
}

func New(o Options) (*Client, error) {
	if o.APIKey == "" {
		return nil, errors.New("tavily: TAVILY_API_KEY is not set")
	}
	if o.BaseURL == "" {
		o.BaseURL = DefaultBaseURL
	}
	u, err := url.Parse(o.BaseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("tavily: invalid base URL %q", o.BaseURL)
	}
	c := &Client{
		apiKey:      o.APIKey,
		endpoint:    strings.TrimSuffix(o.BaseURL, "/") + "/search",
		http:        o.HTTPClient,
		log:         o.Logger,
		maxAttempts: 3,
		sleep:       sleepCtx,
	}
	if c.http == nil {
		c.http = &http.Client{Timeout: 30 * time.Second}
	}
	if c.log == nil {
		c.log = slog.New(slog.DiscardHandler)
	}
	return c, nil
}

type Query struct {
	Text           string
	Topic          string
	TimeRange      string
	MaxResults     int
	IncludeDomains []string
}

type Result struct {
	Title         string  `json:"title"`
	URL           string  `json:"url"`
	Content       string  `json:"content"`
	Score         float64 `json:"score"`
	PublishedDate string  `json:"published_date"`
}

type Response struct {
	Query        string   `json:"query"`
	Answer       string   `json:"answer"`
	Results      []Result `json:"results"`
	ResponseTime float64  `json:"response_time"`
	Usage        struct {
		Credits float64 `json:"credits"`
	} `json:"usage"`
}

type searchRequest struct {
	Query          string   `json:"query"`
	Topic          string   `json:"topic"`
	SearchDepth    string   `json:"search_depth"`
	MaxResults     int      `json:"max_results"`
	IncludeAnswer  bool     `json:"include_answer"`
	IncludeUsage   bool     `json:"include_usage"`
	TimeRange      string   `json:"time_range,omitempty"`
	IncludeDomains []string `json:"include_domains,omitempty"`
}

func (q Query) normalize() (searchRequest, error) {
	req := searchRequest{
		Query:          strings.Join(strings.Fields(q.Text), " "),
		Topic:          q.Topic,
		SearchDepth:    "basic",
		MaxResults:     q.MaxResults,
		IncludeAnswer:  true,
		IncludeUsage:   true,
		TimeRange:      q.TimeRange,
		IncludeDomains: q.IncludeDomains,
	}
	if req.Query == "" {
		return req, errors.New("tavily: query is empty")
	}
	if utf8.RuneCountInString(req.Query) > MaxQueryLen {
		return req, fmt.Errorf("tavily: query is longer than %d characters", MaxQueryLen)
	}
	if req.Topic == "" {
		req.Topic = "general"
	}
	if !slices.Contains(Topics, req.Topic) {
		return req, fmt.Errorf("tavily: topic must be one of %s", strings.Join(Topics, ", "))
	}
	if req.TimeRange != "" && !slices.Contains(TimeRanges, req.TimeRange) {
		return req, fmt.Errorf("tavily: time range must be one of %s", strings.Join(TimeRanges, ", "))
	}
	if req.MaxResults == 0 {
		req.MaxResults = 5
	}
	if req.MaxResults < 1 || req.MaxResults > MaxResults {
		return req, fmt.Errorf("tavily: max results must be between 1 and %d", MaxResults)
	}
	return req, nil
}

func (c *Client) Search(ctx context.Context, q Query) (*Response, error) {
	req, err := q.normalize()
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("tavily: encode request: %w", err)
	}

	var out Response
	for attempt := 1; ; attempt++ {
		err = c.send(ctx, body, &out)
		if err == nil || attempt == c.maxAttempts || !retryable(err) {
			break
		}
		c.log.Debug("retrying tavily search", "attempt", attempt, "err", err)
		if err := c.sleep(ctx, backoff(attempt, err)); err != nil {
			return nil, err
		}
	}
	if err != nil {
		return nil, err
	}
	c.log.Debug("tavily search", "results", len(out.Results), "credits", out.Usage.Credits, "seconds", out.ResponseTime)
	return &out, nil
}

func (c *Client) send(ctx context.Context, body []byte, out *Response) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

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
	*out = Response{}
	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		return fmt.Errorf("tavily: decode response: %w", err)
	}
	return nil
}

func readAPIError(res *http.Response) *APIError {
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
	e := &APIError{Status: res.StatusCode, Message: strings.TrimSpace(string(raw))}

	var body struct {
		Detail json.RawMessage `json:"detail"`
	}
	if json.Unmarshal(raw, &body) == nil && len(body.Detail) > 0 {
		var withError struct {
			Error string `json:"error"`
		}
		var text string
		switch {
		case json.Unmarshal(body.Detail, &withError) == nil && withError.Error != "":
			e.Message = withError.Error
		case json.Unmarshal(body.Detail, &text) == nil && text != "":
			e.Message = text
		default:
			e.Message = string(body.Detail)
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
