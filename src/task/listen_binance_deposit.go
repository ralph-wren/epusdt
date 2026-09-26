package task

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/GMWalletApp/epusdt/model/dao"
	"github.com/GMWalletApp/epusdt/model/data"
	"github.com/GMWalletApp/epusdt/model/mdb"
	"github.com/GMWalletApp/epusdt/model/service"
	"github.com/GMWalletApp/epusdt/util/log"
	"github.com/shopspring/decimal"
)

const (
	binanceAPIBaseURL       = "https://api.binance.com"
	binanceDepositPath      = "/sapi/v1/capital/deposit/hisrec"
	binanceDepositPageLimit = 1000
	binanceMaxResponseBytes = 1 << 20
	binanceRequestTimeout   = 10 * time.Second
)

type binanceDepositConfig struct {
	Enabled      bool
	APIKey       string
	SecretKey    string
	PollInterval time.Duration
	Lookback     time.Duration
}

type binanceDeposit struct {
	ID           string `json:"id"`
	Amount       string `json:"amount"`
	Coin         string `json:"coin"`
	Network      string `json:"network"`
	Address      string `json:"address"`
	TxID         string `json:"txId"`
	InsertTime   int64  `json:"insertTime"`
	CompleteTime int64  `json:"completeTime"`
	TransferType int    `json:"transferType"`
	Status       int    `json:"status"`
}

type binanceDepositClient struct {
	baseURL   string
	http      *http.Client
	apiKey    string
	secretKey string
	now       func() time.Time
}

type binanceDepositLister interface {
	ListDeposits(ctx context.Context, start, end time.Time, offset, limit int) ([]binanceDeposit, error)
}

type binanceDepositProcessor func(depositID, network, address, token string, amount float64, paidAtMs int64) (bool, error)

func newBinanceDepositClient(baseURL string, httpClient *http.Client, apiKey, secretKey string, now func() time.Time) *binanceDepositClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: binanceRequestTimeout}
	}
	if now == nil {
		now = time.Now
	}
	return &binanceDepositClient{
		baseURL:   strings.TrimRight(baseURL, "/"),
		http:      httpClient,
		apiKey:    strings.TrimSpace(apiKey),
		secretKey: strings.TrimSpace(secretKey),
		now:       now,
	}
}

func (c *binanceDepositClient) ListDeposits(ctx context.Context, start, end time.Time, offset, limit int) ([]binanceDeposit, error) {
	query := url.Values{}
	query.Set("coin", "USDT")
	query.Set("endTime", strconv.FormatInt(end.UnixMilli(), 10))
	query.Set("limit", strconv.Itoa(limit))
	query.Set("offset", strconv.Itoa(offset))
	query.Set("recvWindow", "5000")
	query.Set("startTime", strconv.FormatInt(start.UnixMilli(), 10))
	query.Set("timestamp", strconv.FormatInt(c.now().UnixMilli(), 10))
	unsigned := query.Encode()
	mac := hmac.New(sha256.New, []byte(c.secretKey))
	_, _ = mac.Write([]byte(unsigned))
	signature := hex.EncodeToString(mac.Sum(nil))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+binanceDepositPath+"?"+unsigned+"&signature="+signature, nil)
	if err != nil {
		return nil, fmt.Errorf("create Binance deposit request: %w", err)
	}
	req.Header.Set("X-MBX-APIKEY", c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("call Binance deposit history: timeout")
		}
		var netErr net.Error
		if errors.As(err, &netErr) {
			return nil, fmt.Errorf("call Binance deposit history: network error (temporary=%t)", netErr.Temporary())
		}
		return nil, fmt.Errorf("call Binance deposit history: request failed")
	}
	defer resp.Body.Close()

	body := io.LimitReader(resp.Body, binanceMaxResponseBytes)
	if resp.StatusCode != http.StatusOK {
		var apiErr struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		_ = json.NewDecoder(body).Decode(&apiErr)
		if apiErr.Code != 0 || strings.TrimSpace(apiErr.Msg) != "" {
			return nil, fmt.Errorf("Binance deposit history rejected: status=%d code=%d message=%s", resp.StatusCode, apiErr.Code, strings.TrimSpace(apiErr.Msg))
		}
		return nil, fmt.Errorf("Binance deposit history rejected: status=%d", resp.StatusCode)
	}

	var deposits []binanceDeposit
	if err = json.NewDecoder(body).Decode(&deposits); err != nil {
		return nil, fmt.Errorf("decode Binance deposit history: %w", err)
	}
	return deposits, nil
}

func loadBinanceDepositConfig() binanceDepositConfig {
	config := binanceDepositConfig{
		PollInterval: time.Duration(mdb.SettingDefaultBinancePollInterval) * time.Second,
		Lookback:     time.Duration(mdb.SettingDefaultBinanceLookback) * time.Minute,
	}
	if dao.Mdb == nil {
		return config
	}

	config.Enabled = data.GetBinanceDepositMonitorEnabled()
	config.APIKey = data.GetBinanceAPIKey()
	config.SecretKey = data.GetBinanceSecretKey()
	pollSeconds := data.GetBinancePollIntervalSeconds()
	if pollSeconds >= 5 && pollSeconds <= 300 {
		config.PollInterval = time.Duration(pollSeconds) * time.Second
	}
	lookbackMinutes := data.GetBinanceLookbackMinutes()
	if lookbackMinutes >= 1 && lookbackMinutes <= 1440 {
		config.Lookback = time.Duration(lookbackMinutes) * time.Minute
	}
	config.Enabled = config.Enabled && config.APIKey != "" && config.SecretKey != ""
	return config
}

func mapBinanceDepositNetwork(network string) (string, bool) {
	switch strings.ToUpper(strings.TrimSpace(network)) {
	case "TRX", "TRON":
		return mdb.NetworkTron, true
	case "ETH", "ERC20", "ETHEREUM":
		return mdb.NetworkEthereum, true
	case "BSC", "BEP20", "BNB SMART CHAIN (BEP20)":
		return mdb.NetworkBsc, true
	case "SOL", "SOLANA":
		return mdb.NetworkSolana, true
	case "POLYGON", "MATIC":
		return mdb.NetworkPolygon, true
	case "APTOS", "APT":
		return mdb.NetworkAptos, true
	case "TON":
		return mdb.NetworkTon, true
	default:
		return "", false
	}
}

func StartBinanceDepositListener() {
	for {
		config := loadBinanceDepositConfig()
		active := false
		if config.Enabled {
			var err error
			active, err = data.HasActiveBinanceDepositOrders()
			if err != nil {
				log.Sugar.Warnf("[binance-deposit] check active orders: %v", err)
			}
		}
		if active {
			if err := pollBinanceDeposits(context.Background(), config); err != nil {
				log.Sugar.Errorf("[binance-deposit] poll failed: %v", err)
			}
		}
		time.Sleep(binanceDepositPollDelay(config, active))
	}
}

func binanceDepositPollDelay(config binanceDepositConfig, active bool) time.Duration {
	if active {
		return config.PollInterval
	}
	return activeOrderPollInterval
}

func pollBinanceDeposits(ctx context.Context, config binanceDepositConfig) error {
	if !config.Enabled || strings.TrimSpace(config.APIKey) == "" || strings.TrimSpace(config.SecretKey) == "" {
		return nil
	}
	now := time.Now()
	client := newBinanceDepositClient(binanceAPIBaseURL, &http.Client{Timeout: binanceRequestTimeout}, config.APIKey, config.SecretKey, time.Now)
	return pollBinanceDepositsWithClient(ctx, config, client, now, service.ProcessBinanceDeposit)
}

func pollBinanceDepositsWithClient(ctx context.Context, config binanceDepositConfig, client binanceDepositLister, now time.Time, process binanceDepositProcessor) error {
	for offset := 0; ; offset += binanceDepositPageLimit {
		deposits, err := client.ListDeposits(ctx, now.Add(-config.Lookback), now, offset, binanceDepositPageLimit)
		if err != nil {
			return err
		}
		for _, deposit := range deposits {
			if deposit.Status != 1 || !strings.EqualFold(strings.TrimSpace(deposit.Coin), "USDT") {
				continue
			}
			network, ok := mapBinanceDepositNetwork(deposit.Network)
			if !ok {
				continue
			}
			amountDecimal, err := decimal.NewFromString(strings.TrimSpace(deposit.Amount))
			if err != nil || amountDecimal.Sign() <= 0 {
				continue
			}
			paidAtMs := deposit.CompleteTime
			if paidAtMs <= 0 {
				paidAtMs = deposit.InsertTime
			}
			processed, err := process(deposit.ID, network, deposit.Address, deposit.Coin, amountDecimal.InexactFloat64(), paidAtMs)
			if err != nil {
				log.Sugar.Errorf("[binance-deposit] process deposit id=%s failed: %v", deposit.ID, err)
				continue
			}
			if processed {
				log.Sugar.Infof("[binance-deposit] payment processed deposit_id=%s", deposit.ID)
			}
		}
		if len(deposits) < binanceDepositPageLimit {
			return nil
		}
	}
}
