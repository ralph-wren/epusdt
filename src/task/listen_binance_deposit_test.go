package task

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/GMWalletApp/epusdt/model/mdb"
)

type failingBinanceTransport struct{}

func (failingBinanceTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("transport failed")
}

func TestBinanceDepositClientSignsAndParsesHistory(t *testing.T) {
	fixedNow := time.Date(2026, 9, 21, 7, 0, 0, 0, time.UTC)
	start := fixedNow.Add(-30 * time.Minute)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sapi/v1/capital/deposit/hisrec" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("X-MBX-APIKEY"); got != "test-api-key" {
			t.Fatalf("X-MBX-APIKEY = %q", got)
		}

		query := r.URL.Query()
		if query.Get("coin") != "USDT" {
			t.Fatalf("coin = %q", query.Get("coin"))
		}
		if query.Get("startTime") != "1789972200000" || query.Get("endTime") != "1789974000000" {
			t.Fatalf("unexpected time window: %s", r.URL.RawQuery)
		}
		if query.Get("timestamp") != "1789974000000" || query.Get("recvWindow") != "5000" {
			t.Fatalf("unexpected signing fields: %s", r.URL.RawQuery)
		}

		unsigned := r.URL.RawQuery
		signature := query.Get("signature")
		marker := "&signature=" + signature
		if len(signature) == 0 || len(unsigned) <= len(marker) || unsigned[len(unsigned)-len(marker):] != marker {
			t.Fatalf("signature must be the final query field: %s", unsigned)
		}
		unsigned = unsigned[:len(unsigned)-len(marker)]
		mac := hmac.New(sha256.New, []byte("test-secret-key"))
		_, _ = mac.Write([]byte(unsigned))
		if want := hex.EncodeToString(mac.Sum(nil)); signature != want {
			t.Fatalf("signature = %q, want %q", signature, want)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"5234244468261465857","amount":"75.08","coin":"USDT","network":"TRX","address":"TGR5A8RXKKTbedDeqd8YRewFjkCXwNLEZB","txId":"Off-chain transfer 413136856735","insertTime":1789972482000,"completeTime":1789972517000,"transferType":1,"status":1}]`))
	}))
	defer server.Close()

	client := newBinanceDepositClient(server.URL, server.Client(), "test-api-key", "test-secret-key", func() time.Time {
		return fixedNow
	})
	deposits, err := client.ListDeposits(context.Background(), start, fixedNow, 0, 1000)
	if err != nil {
		t.Fatalf("ListDeposits: %v", err)
	}
	if len(deposits) != 1 {
		t.Fatalf("deposit count = %d, want 1", len(deposits))
	}
	got := deposits[0]
	if got.ID != "5234244468261465857" || got.Amount != "75.08" || got.Network != "TRX" || got.Status != 1 {
		t.Fatalf("unexpected deposit: %#v", got)
	}
}

func TestBinanceDepositClientDoesNotLeakSignedURLInErrors(t *testing.T) {
	client := newBinanceDepositClient("https://api.binance.com", &http.Client{Transport: failingBinanceTransport{}}, "sensitive-api-key", "sensitive-secret", time.Now)
	_, err := client.ListDeposits(context.Background(), time.Now().Add(-time.Minute), time.Now(), 0, 1000)
	if err == nil {
		t.Fatal("ListDeposits error = nil")
	}
	message := err.Error()
	for _, forbidden := range []string{"signature=", "sensitive-api-key", "sensitive-secret", "api.binance.com"} {
		if strings.Contains(message, forbidden) {
			t.Fatalf("error leaked %q: %s", forbidden, message)
		}
	}
}

func TestLoadBinanceDepositConfigDisabledWithoutCredentials(t *testing.T) {
	config := loadBinanceDepositConfig()
	if config.Enabled {
		t.Fatal("listener must stay disabled when settings are absent")
	}
	if config.APIKey != "" || config.SecretKey != "" {
		t.Fatal("credentials must default to empty")
	}
}

func TestBinanceDepositListenerSleepsUntilOrderAndStopsWhenIdle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wakeup := make(chan struct{}, 1)
	checks := make(chan struct{}, 10)
	polls := make(chan struct{}, 10)
	done := make(chan struct{})
	var active atomic.Bool
	go func() {
		defer close(done)
		runBinanceDepositListener(ctx, wakeup,
			func() binanceDepositConfig {
				return binanceDepositConfig{Enabled: true, PollInterval: 30 * time.Millisecond}
			},
			func() (bool, error) { checks <- struct{}{}; return active.Load(), nil },
			func(context.Context, binanceDepositConfig) error { polls <- struct{}{}; return nil })
	}()
	wait := func(ch <-chan struct{}, label string) {
		t.Helper()
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for %s", label)
		}
	}
	wait(checks, "startup check")
	select {
	case <-checks:
		t.Fatal("idle listener checked orders without notification")
	case <-time.After(80 * time.Millisecond):
	}
	active.Store(true)
	wakeup <- struct{}{}
	wait(checks, "order notification")
	wait(polls, "immediate deposit poll")
	wait(checks, "active interval")
	wait(polls, "next deposit poll")
	active.Store(false)
	wait(checks, "order completion")
	select {
	case <-checks:
		t.Fatal("idle listener resumed checking orders without notification")
	case <-time.After(80 * time.Millisecond):
	}
	cancel()
	wait(done, "listener shutdown")
}

func TestPollBinanceDepositsSkipsDisabledOrIncompleteConfiguration(t *testing.T) {
	configs := []binanceDepositConfig{
		{Enabled: false, APIKey: "unused", SecretKey: "unused"},
		{Enabled: true, APIKey: "", SecretKey: "missing-api-key"},
		{Enabled: true, APIKey: "missing-secret", SecretKey: ""},
	}
	for _, config := range configs {
		if err := pollBinanceDeposits(context.Background(), config); err != nil {
			t.Fatalf("pollBinanceDeposits(%#v): %v", config, err)
		}
	}
}

func TestMapBinanceDepositNetwork(t *testing.T) {
	tests := map[string]string{
		"TRX":     mdb.NetworkTron,
		"ETH":     mdb.NetworkEthereum,
		"BSC":     mdb.NetworkBsc,
		"SOL":     mdb.NetworkSolana,
		"POLYGON": mdb.NetworkPolygon,
		"APTOS":   mdb.NetworkAptos,
		"TON":     mdb.NetworkTon,
	}
	for input, want := range tests {
		if got, ok := mapBinanceDepositNetwork(input); !ok || got != want {
			t.Fatalf("mapBinanceDepositNetwork(%q) = (%q, %v), want (%q, true)", input, got, ok, want)
		}
	}
	if got, ok := mapBinanceDepositNetwork("unknown"); ok || got != "" {
		t.Fatalf("unknown network = (%q, %v), want empty false", got, ok)
	}
}

type fakeBinanceDepositLister struct {
	pages   map[int][]binanceDeposit
	offsets []int
}

func (f *fakeBinanceDepositLister) ListDeposits(_ context.Context, _, _ time.Time, offset, _ int) ([]binanceDeposit, error) {
	f.offsets = append(f.offsets, offset)
	return f.pages[offset], nil
}

func TestPollBinanceDepositsFiltersPagesAndFallsBackToInsertTime(t *testing.T) {
	firstPage := make([]binanceDeposit, binanceDepositPageLimit)
	firstPage[0] = binanceDeposit{ID: "valid", Amount: "75.08", Coin: "USDT", Network: "TRX", Address: "TAddress", InsertTime: 1000, Status: 1}
	firstPage[1] = binanceDeposit{ID: "pending", Amount: "1", Coin: "USDT", Network: "TRX", Address: "TAddress", InsertTime: 1000, Status: 0}
	firstPage[2] = binanceDeposit{ID: "wrong-coin", Amount: "1", Coin: "USDC", Network: "TRX", Address: "TAddress", InsertTime: 1000, Status: 1}
	firstPage[3] = binanceDeposit{ID: "unknown-network", Amount: "1", Coin: "USDT", Network: "UNKNOWN", Address: "TAddress", InsertTime: 1000, Status: 1}
	firstPage[4] = binanceDeposit{ID: "bad-amount", Amount: "not-a-number", Coin: "USDT", Network: "TRX", Address: "TAddress", InsertTime: 1000, Status: 1}
	client := &fakeBinanceDepositLister{pages: map[int][]binanceDeposit{
		0:                       firstPage,
		binanceDepositPageLimit: {},
	}}

	var processed []binanceDeposit
	err := pollBinanceDepositsWithClient(context.Background(), binanceDepositConfig{Lookback: 30 * time.Minute}, client, time.UnixMilli(5000), func(id, network, address, token string, amount float64, paidAtMs int64) (bool, error) {
		processed = append(processed, binanceDeposit{ID: id, Network: network, Address: address, Coin: token, Amount: "75.08", CompleteTime: paidAtMs})
		return true, nil
	})
	if err != nil {
		t.Fatalf("pollBinanceDepositsWithClient: %v", err)
	}
	if len(client.offsets) != 2 || client.offsets[0] != 0 || client.offsets[1] != binanceDepositPageLimit {
		t.Fatalf("offsets = %v", client.offsets)
	}
	if len(processed) != 1 {
		t.Fatalf("processed = %#v", processed)
	}
	if processed[0].ID != "valid" || processed[0].Network != mdb.NetworkTron || processed[0].CompleteTime != 1000 {
		t.Fatalf("processed deposit = %#v", processed[0])
	}
}
