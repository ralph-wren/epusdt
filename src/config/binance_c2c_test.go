package config

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestGetPaymentRateForCoinUsesLatestBinanceC2CMedian(t *testing.T) {
	installSettingsGetter(t, map[string]string{
		"rate.mode":                          RateModeFixed,
		"rate.forced_rate_list":              `{"cny":{"usdt":0.14705882352941177}}`,
		"rate.binance_c2c_enabled":           "true",
		"rate.binance_c2c_cache_ttl_seconds": "60",
	})
	installMockHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read Binance request: %v", err)
		}
		var rawRequest map[string]interface{}
		if err := json.Unmarshal(body, &rawRequest); err != nil {
			t.Fatalf("decode raw Binance request: %v", err)
		}
		if _, exists := rawRequest["transAmount"]; exists {
			t.Fatalf("Binance request must not contain transAmount: %s", body)
		}
		var request binanceC2CSearchRequest
		if err := json.NewDecoder(bytes.NewReader(body)).Decode(&request); err != nil {
			t.Fatalf("decode Binance request: %v", err)
		}
		if request.Asset != "USDT" || request.Fiat != "CNY" || request.TradeType != "BUY" || request.Rows != 10 {
			t.Fatalf("unexpected Binance request: %#v", request)
		}
		return binanceResponse(r, `{
			"code":"000000","message":null,"success":true,"data":[
				{"adv":{"price":"6.75"}},
				{"adv":{"price":"6.67"}},
				{"adv":{"price":"6.72"}},
				{"adv":{"price":"6.69"}},
				{"adv":{"price":"6.73"}},
				{"adv":{"price":"6.66"}},
				{"adv":{"price":"6.74"}},
				{"adv":{"price":"6.71"}},
				{"adv":{"price":"6.68"}},
				{"adv":{"price":"6.70"}}
			]}`), nil
	})

	want := 1.0 / 6.705
	if got := GetPaymentRateForCoin("USDT", "CNY", 10); math.Abs(got-want) > 1e-12 {
		t.Fatalf("payment rate = %.12f, want %.12f", got, want)
	}
}

func TestGetPaymentRateForCoinCachesBinanceQuote(t *testing.T) {
	installSettingsGetter(t, map[string]string{
		"rate.mode":                          RateModeFixed,
		"rate.forced_rate_list":              `{"cny":{"usdt":0.14705882352941177}}`,
		"rate.binance_c2c_enabled":           "true",
		"rate.binance_c2c_cache_ttl_seconds": "60",
	})
	var calls atomic.Int32
	installMockHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return binanceResponse(r, eligibleBinanceResponse("6.66", "6.67", "6.68", "6.69", "6.70", "6.71", "6.72", "6.73", "6.74", "6.75")), nil
	})

	first := GetPaymentRateForCoin("usdt", "cny", 10)
	second := GetPaymentRateForCoin("usdt", "cny", 100000)
	if first != second || calls.Load() != 1 {
		t.Fatalf("cached rates = %v/%v, calls = %d", first, second, calls.Load())
	}
}

func TestGetPaymentRateForCoinRejectsOutlyingBinanceQuote(t *testing.T) {
	fallback := 1.0 / 6.8
	installSettingsGetter(t, map[string]string{
		"rate.mode":                          RateModeFixed,
		"rate.forced_rate_list":              `{"cny":{"usdt":0.14705882352941177}}`,
		"rate.binance_c2c_enabled":           "true",
		"rate.binance_c2c_cache_ttl_seconds": "60",
	})
	installMockHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		return binanceResponse(r, eligibleBinanceResponse("5.00", "5.01", "5.02", "5.03", "5.04", "5.05", "5.06", "5.07", "5.08", "5.09")), nil
	})

	if got := GetPaymentRateForCoin("usdt", "cny", 100); math.Abs(got-fallback) > 1e-12 {
		t.Fatalf("outlier fallback rate = %.12f, want %.12f", got, fallback)
	}
}

func TestGetPaymentRateForCoinFallsBackWhenBinanceDisabled(t *testing.T) {
	fallback := 1.0 / 6.8
	installSettingsGetter(t, map[string]string{
		"rate.mode":                RateModeFixed,
		"rate.forced_rate_list":    `{"cny":{"usdt":0.14705882352941177}}`,
		"rate.binance_c2c_enabled": "false",
	})
	installMockHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		t.Fatal("Binance must not be called when disabled")
		return nil, nil
	})

	if got := GetPaymentRateForCoin("usdt", "cny", 100); math.Abs(got-fallback) > 1e-12 {
		t.Fatalf("disabled fallback rate = %.12f, want %.12f", got, fallback)
	}
}

func binanceResponse(r *http.Request, body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    r,
	}
}

func eligibleBinanceResponse(prices ...string) string {
	rows := make([]string, 0, len(prices))
	for _, price := range prices {
		rows = append(rows, `{"adv":{"price":"`+price+`"}}`)
	}
	return `{"code":"000000","message":null,"success":true,"data":[` + strings.Join(rows, ",") + `]}`
}
