package config

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/GMWalletApp/epusdt/util/http_client"
	"golang.org/x/sync/singleflight"
)

const (
	defaultBinanceC2CSearchURL       = "https://p2p.binance.com/bapi/c2c/v2/friendly/c2c/adv/search"
	defaultBinanceC2CCacheTTLSeconds = 60
	binanceC2CQuoteCount             = 3
	binanceC2CMinimumMonthlyOrders   = 20
	binanceC2CMinimumCompletionRate  = 0.90
	binanceC2CMaximumRateDeviation   = 0.10
	binanceC2CMinimumReasonablePrice = 1.0
	binanceC2CMaximumReasonablePrice = 20.0
)

type binanceC2CCacheEntry struct {
	price     float64
	expiresAt time.Time
}

type binanceC2CSearchRequest struct {
	Page          int      `json:"page"`
	Rows          int      `json:"rows"`
	PayTypes      []string `json:"payTypes"`
	PublisherType *string  `json:"publisherType"`
	Asset         string   `json:"asset"`
	TradeType     string   `json:"tradeType"`
	Fiat          string   `json:"fiat"`
	TransAmount   string   `json:"transAmount"`
}

type binanceC2CSearchResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Success bool   `json:"success"`
	Data    []struct {
		Adv struct {
			Price                       string `json:"price"`
			MinSingleTransAmount        string `json:"minSingleTransAmount"`
			DynamicMaxSingleTransAmount string `json:"dynamicMaxSingleTransAmount"`
		} `json:"adv"`
		Advertiser struct {
			MonthOrderCount int     `json:"monthOrderCount"`
			MonthFinishRate float64 `json:"monthFinishRate"`
		} `json:"advertiser"`
	} `json:"data"`
}

var (
	binanceC2CSearchURL = defaultBinanceC2CSearchURL
	binanceC2CCacheMu   sync.RWMutex
	binanceC2CCache     = make(map[string]binanceC2CCacheEntry)
	binanceC2CRequests  singleflight.Group
)

// GetPaymentRateForCoin returns coin units per one base-currency unit. When
// enabled, USDT/CNY payments use an amount-matched Binance C2C BUY quote and
// retain the existing configured rate as a safety fallback.
func GetPaymentRateForCoin(coin, base string, fiatAmount float64) float64 {
	coin = normalizeRateKey(coin)
	base = normalizeRateKey(base)
	fallbackRate := GetRateForCoin(coin, base)
	if coin != "usdt" || base != "cny" || fiatAmount <= 0 || !isBinanceC2CEnabled() {
		return fallbackRate
	}

	price, err := getBinanceC2CPrice(fiatAmount)
	if err != nil {
		log.Printf("binance C2C rate unavailable, using configured fallback: %s", err)
		return fallbackRate
	}
	if fallbackRate > 0 {
		fallbackPrice := 1 / fallbackRate
		deviation := math.Abs(price-fallbackPrice) / fallbackPrice
		if deviation > binanceC2CMaximumRateDeviation {
			log.Printf("binance C2C price %.4f rejected: deviation %.2f%% exceeds limit", price, deviation*100)
			return fallbackRate
		}
	}
	return 1 / price
}

func isBinanceC2CEnabled() bool {
	enabled, err := strconv.ParseBool(strings.TrimSpace(settingsRateBinanceC2CEnabled()))
	return err == nil && enabled
}

func getBinanceC2CCacheTTLSeconds() int {
	ttl, err := strconv.Atoi(strings.TrimSpace(settingsRateBinanceC2CCacheTTLSeconds()))
	if err != nil || ttl < MinRateCacheTTLSeconds || ttl > MaxRateCacheTTLSeconds {
		return defaultBinanceC2CCacheTTLSeconds
	}
	return ttl
}

func getBinanceC2CPrice(fiatAmount float64) (float64, error) {
	cacheKey := strconv.FormatFloat(fiatAmount, 'f', 2, 64)
	now := rateNow()
	if price, ok := loadBinanceC2CCache(cacheKey, now); ok {
		return price, nil
	}

	value, err, _ := binanceC2CRequests.Do(cacheKey, func() (interface{}, error) {
		now := rateNow()
		if price, ok := loadBinanceC2CCache(cacheKey, now); ok {
			return price, nil
		}
		price, err := fetchBinanceC2CPrice(cacheKey, fiatAmount)
		if err != nil {
			return 0.0, err
		}
		binanceC2CCacheMu.Lock()
		binanceC2CCache[cacheKey] = binanceC2CCacheEntry{
			price:     price,
			expiresAt: now.Add(time.Duration(getBinanceC2CCacheTTLSeconds()) * time.Second),
		}
		binanceC2CCacheMu.Unlock()
		return price, nil
	})
	if err != nil {
		return 0, err
	}
	return value.(float64), nil
}

func fetchBinanceC2CPrice(transAmount string, fiatAmount float64) (float64, error) {
	payload := binanceC2CSearchRequest{
		Page:        1,
		Rows:        10,
		PayTypes:    []string{},
		Asset:       "USDT",
		TradeType:   "BUY",
		Fiat:        "CNY",
		TransAmount: transAmount,
	}
	resp, err := http_client.GetHttpClient().R().
		SetHeader("Accept", "application/json").
		SetHeader("Content-Type", "application/json").
		SetHeader("User-Agent", "Mozilla/5.0 (compatible; EPUSDT/2.0; +https://epusdt.com/)").
		SetHeader("Origin", "https://p2p.binance.com").
		SetHeader("Referer", "https://p2p.binance.com/").
		SetBody(payload).
		Post(binanceC2CSearchURL)
	if err != nil {
		return 0, fmt.Errorf("call Binance C2C: %w", err)
	}
	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return 0, fmt.Errorf("call Binance C2C unexpected status: %s", resp.Status())
	}

	var result binanceC2CSearchResponse
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		return 0, fmt.Errorf("decode Binance C2C response: %w", err)
	}
	if !result.Success || result.Code != "000000" {
		return 0, fmt.Errorf("Binance C2C rejected request: code=%s message=%s", result.Code, result.Message)
	}

	prices := make([]float64, 0, binanceC2CQuoteCount)
	for _, row := range result.Data {
		price, priceErr := strconv.ParseFloat(strings.TrimSpace(row.Adv.Price), 64)
		minAmount, minErr := strconv.ParseFloat(strings.TrimSpace(row.Adv.MinSingleTransAmount), 64)
		maxAmount, maxErr := strconv.ParseFloat(strings.TrimSpace(row.Adv.DynamicMaxSingleTransAmount), 64)
		if priceErr != nil || minErr != nil || maxErr != nil || price < binanceC2CMinimumReasonablePrice || price > binanceC2CMaximumReasonablePrice {
			continue
		}
		if fiatAmount < minAmount || fiatAmount > maxAmount {
			continue
		}
		if row.Advertiser.MonthOrderCount < binanceC2CMinimumMonthlyOrders || row.Advertiser.MonthFinishRate < binanceC2CMinimumCompletionRate {
			continue
		}
		prices = append(prices, price)
		if len(prices) == binanceC2CQuoteCount {
			break
		}
	}
	if len(prices) < binanceC2CQuoteCount {
		return 0, fmt.Errorf("Binance C2C returned only %d eligible amount-matched quotes", len(prices))
	}
	sort.Float64s(prices)
	return prices[len(prices)/2], nil
}

func loadBinanceC2CCache(key string, now time.Time) (float64, bool) {
	binanceC2CCacheMu.RLock()
	entry, ok := binanceC2CCache[key]
	binanceC2CCacheMu.RUnlock()
	return entry.price, ok && now.Before(entry.expiresAt)
}

func resetBinanceC2CCache() {
	binanceC2CCacheMu.Lock()
	binanceC2CCache = make(map[string]binanceC2CCacheEntry)
	binanceC2CCacheMu.Unlock()
}
