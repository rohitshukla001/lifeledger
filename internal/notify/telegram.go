package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	DefaultTelegramURL = "https://api.telegram.org"
	MaxMessageRunes    = 4096
)

type TelegramOptions struct {
	Token      string
	ChatID     string
	BaseURL    string
	HTTPClient *http.Client
}

type Telegram struct {
	endpoint string
	chatID   string
	http     *http.Client
}

func NewTelegram(o TelegramOptions) (*Telegram, error) {
	if o.Token == "" || o.ChatID == "" {
		return nil, errors.New("notify: TELEGRAM_BOT_TOKEN and TELEGRAM_CHAT_ID are both required")
	}
	if strings.ContainsAny(o.Token, "/?#") {
		return nil, errors.New("notify: TELEGRAM_BOT_TOKEN has characters that a bot token cannot have")
	}
	if o.BaseURL == "" {
		o.BaseURL = DefaultTelegramURL
	}
	t := &Telegram{
		endpoint: strings.TrimSuffix(o.BaseURL, "/") + "/bot" + o.Token + "/sendMessage",
		chatID:   o.ChatID,
		http:     o.HTTPClient,
	}
	if t.http == nil {
		t.http = &http.Client{Timeout: 20 * time.Second}
	}
	return t, nil
}

func (t *Telegram) Send(ctx context.Context, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("notify: message is empty")
	}
	if utf8.RuneCountInString(text) > MaxMessageRunes {
		text = string([]rune(text)[:MaxMessageRunes-3]) + "..."
	}
	body, err := json.Marshal(map[string]any{
		"chat_id":              t.chatID,
		"text":                 text,
		"link_preview_options": map[string]bool{"is_disabled": true},
	})
	if err != nil {
		return fmt.Errorf("notify: encode message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, bytes.NewReader(body))
	if err != nil {
		return errors.New("notify: cannot build the Telegram request")
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := t.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return fmt.Errorf("telegram: %w", err)
	}
	defer res.Body.Close()

	var out struct {
		OK          bool   `json:"ok"`
		ErrorCode   int    `json:"error_code"`
		Description string `json:"description"`
	}
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	if err := json.Unmarshal(raw, &out); err != nil {
		return fmt.Errorf("telegram: HTTP %d with a body that is not JSON", res.StatusCode)
	}
	if !out.OK {
		if out.ErrorCode == 0 {
			out.ErrorCode = res.StatusCode
		}
		return fmt.Errorf("telegram: %d %s", out.ErrorCode, out.Description)
	}
	return nil
}
