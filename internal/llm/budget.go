package llm

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var ErrBudgetExceeded = errors.New("llm: daily spend limit reached")

type price struct {
	inputPerM  float64
	outputPerM float64
}

func (p price) cost(u Usage) float64 {
	return (float64(u.PromptTokens)*p.inputPerM + float64(u.CompletionTokens)*p.outputPerM) / 1e6
}

var prices = map[string]price{
	"nvidia/nvidia-nemotron-3-nano-30b-a3b": {0.06, 0.24},
	"nvidia/nemotron-3-super-120b-a12b":     {0.30, 0.90},
	"nvidia/nemotron-3-ultra-550b-a55b":     {1.00, 3.00},
	"qwen/qwen3-embedding-8b":               {0.01, 0},
}

func priceFor(model string) price {
	if p, ok := prices[strings.ToLower(model)]; ok {
		return p
	}
	return prices["nvidia/nemotron-3-ultra-550b-a55b"]
}

type Budget struct {
	mu    sync.Mutex
	limit float64
	now   func() time.Time
	day   string
	spent float64
}

func NewBudget(dailyLimitUSD float64) *Budget {
	return &Budget{limit: dailyLimitUSD, now: time.Now}
}

func (b *Budget) Spent() float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rollover()
	return b.spent
}

func (b *Budget) check() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rollover()
	if b.limit > 0 && b.spent >= b.limit {
		return fmt.Errorf("%w ($%.4f of $%.2f)", ErrBudgetExceeded, b.spent, b.limit)
	}
	return nil
}

func (b *Budget) add(usd float64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rollover()
	b.spent += usd
}

func (b *Budget) rollover() {
	day := b.now().UTC().Format(time.DateOnly)
	if day != b.day {
		b.day = day
		b.spent = 0
	}
}
