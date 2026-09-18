package config

import (
	"encoding/json"
	"io"
	"math"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestGetPaymentRateForCoinUsesAmountMatchedBinanceC2CMedian(t *testing.T) {
	installSettingsGetter(t, map[string]string{
		"rate.mode":                          RateModeFixed,
		"rate.forced_rate_list":              `{"cny":{"usdt":0.14705882352941177}}`,
		"rate.binance_c2c_enabled":           "true",
		"rate.binance_c2c_cache_ttl_seconds": "60",
	})
	installMockHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		var request binanceC2CSearchRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode Binance request: %v", err)
		}
		if request.TransAmount != "10.00" || request.Asset != "USDT" || request.Fiat != "CNY" || request.TradeType != "BUY" {
			t.Fatalf("unexpected Binance request: %#v", request)
		}
		return binanceResponse(r, `{
			"code":"000000","message":null,"success":true,"data":[
				{"adv":{"price":"6.20","minSingleTransAmount":"10","dynamicMaxSingleTransAmount":"20"},"advertiser":{"monthOrderCount":1,"monthFinishRate":1}},
				{"adv":{"price":"6.66","minSingleTransAmount":"10","dynamicMaxSingleTransAmount":"20"},"advertiser":{"monthOrderCount":500,"monthFinishRate":0.99}},
				{"adv":{"price":"6.68","minSingleTransAmount":"10","dynamicMaxSingleTransAmount":"20"},"advertiser":{"monthOrderCount":500,"monthFinishRate":0.99}},
				{"adv":{"price":"6.80","minSingleTransAmount":"10","dynamicMaxSingleTransAmount":"100"},"advertiser":{"monthOrderCount":500,"monthFinishRate":0.99}}
			]}`), nil
	})

	want := 1.0 / 6.68
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
		return binanceResponse(r, eligibleBinanceResponse("6.66", "6.68", "6.80")), nil
	})

	first := GetPaymentRateForCoin("usdt", "cny", 100)
	second := GetPaymentRateForCoin("usdt", "cny", 100)
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
		return binanceResponse(r, eligibleBinanceResponse("5.00", "5.01", "5.02")), nil
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
		rows = append(rows, `{"adv":{"price":"`+price+`","minSingleTransAmount":"10","dynamicMaxSingleTransAmount":"1000"},"advertiser":{"monthOrderCount":500,"monthFinishRate":0.99}}`)
	}
	return `{"code":"000000","message":null,"success":true,"data":[` + strings.Join(rows, ",") + `]}`
}
