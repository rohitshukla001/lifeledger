package notify_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rohitshukla001/lifeledger/internal/config"
	"github.com/rohitshukla001/lifeledger/internal/notify"
)

func TestLiveTelegramSend(t *testing.T) {
	if os.Getenv("LIFELEDGER_LIVE_TEST") != "1" {
		t.Skip("set LIFELEDGER_LIVE_TEST=1 to send a real Telegram message")
	}
	cfg, err := config.Load(filepath.Join("..", "..", ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TelegramBotToken == "" {
		t.Skip("TELEGRAM_BOT_TOKEN is not set")
	}
	tg, err := notify.NewTelegram(notify.TelegramOptions{Token: cfg.TelegramBotToken, ChatID: cfg.TelegramChatID})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := tg.Send(ctx, "LifeLedger live test: Telegram delivery works."); err != nil {
		t.Fatal(err)
	}
}
