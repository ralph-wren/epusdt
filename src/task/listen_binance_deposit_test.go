package task

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

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

func TestLoadBinanceDepositConfigDisabledWithoutCredentials(t *testing.T) {
	config := loadBinanceDepositConfig()
	if config.Enabled {
		t.Fatal("listener must stay disabled when settings are absent")
	}
	if config.APIKey != "" || config.SecretKey != "" {
		t.Fatal("credentials must default to empty")
	}
}

func TestMapBinanceDepositNetwork(t *testing.T) {
	tests := map[string]string{
		"TRX":     "tron",
		"ETH":     "ethereum",
		"BSC":     "bsc",
		"SOL":     "solana",
		"POLYGON": "polygon",
		"APTOS":   "aptos",
		"TON":     "ton",
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
