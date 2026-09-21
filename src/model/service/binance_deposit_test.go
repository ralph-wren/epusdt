package service

import (
	"testing"
	"time"

	"github.com/GMWalletApp/epusdt/config"
	"github.com/GMWalletApp/epusdt/internal/testutil"
	"github.com/GMWalletApp/epusdt/model/dao"
	"github.com/GMWalletApp/epusdt/model/data"
	"github.com/GMWalletApp/epusdt/model/mdb"
)

func seedBinanceDepositOrder(t *testing.T, tradeID, address string, amount float64, status int, createdAt time.Time) {
	t.Helper()
	order := &mdb.Orders{
		TradeId:        tradeID,
		OrderId:        "order-" + tradeID,
		Amount:         500,
		Currency:       "CNY",
		ActualAmount:   amount,
		Token:          "USDT",
		Network:        mdb.NetworkTron,
		ReceiveAddress: address,
		Status:         status,
		PayProvider:    mdb.PaymentProviderOnChain,
	}
	if err := dao.Mdb.Create(order).Error; err != nil {
		t.Fatalf("seed order: %v", err)
	}
	if err := dao.Mdb.Model(order).Update("created_at", createdAt).Error; err != nil {
		t.Fatalf("set created_at: %v", err)
	}
	if status == mdb.StatusWaitPay {
		if err := data.LockTransaction(mdb.NetworkTron, address, "USDT", tradeID, amount, time.Hour); err != nil {
			t.Fatalf("lock transaction: %v", err)
		}
	}
}

func TestProcessBinanceDepositCompletesMatchingOrderOnce(t *testing.T) {
	cleanup := testutil.SetupTestDatabases(t)
	defer cleanup()

	createdAt := time.Now().Add(-time.Minute)
	seedBinanceDepositOrder(t, "trade-binance-1", "TGR5A8RXKKTbedDeqd8YRewFjkCXwNLEZB", 75.08, mdb.StatusWaitPay, createdAt)

	processed, err := ProcessBinanceDeposit("5234244468261465857", mdb.NetworkTron, "TGR5A8RXKKTbedDeqd8YRewFjkCXwNLEZB", "USDT", 75.08, createdAt.Add(30*time.Second).UnixMilli())
	if err != nil || !processed {
		t.Fatalf("first ProcessBinanceDeposit = (%v, %v), want (true, nil)", processed, err)
	}
	processed, err = ProcessBinanceDeposit("5234244468261465857", mdb.NetworkTron, "TGR5A8RXKKTbedDeqd8YRewFjkCXwNLEZB", "USDT", 75.08, createdAt.Add(30*time.Second).UnixMilli())
	if err != nil || processed {
		t.Fatalf("duplicate ProcessBinanceDeposit = (%v, %v), want (false, nil)", processed, err)
	}

	order, err := data.GetOrderInfoByTradeId("trade-binance-1")
	if err != nil {
		t.Fatalf("load order: %v", err)
	}
	if order.Status != mdb.StatusPaySuccess {
		t.Fatalf("status = %d, want %d", order.Status, mdb.StatusPaySuccess)
	}
	if order.BlockTransactionId != "binance:5234244468261465857" {
		t.Fatalf("block_transaction_id = %q", order.BlockTransactionId)
	}

	var claims int64
	if err := dao.Mdb.Model(&mdb.ProcessedTransaction{}).
		Where("network = ? AND block_transaction_id = ?", "binance", "5234244468261465857").
		Count(&claims).Error; err != nil {
		t.Fatalf("count claims: %v", err)
	}
	if claims != 1 {
		t.Fatalf("claim count = %d, want 1", claims)
	}
}

func TestProcessBinanceDepositRecoversOnlyTimelyExpiredOrder(t *testing.T) {
	cleanup := testutil.SetupTestDatabases(t)
	defer cleanup()

	if err := data.SetSetting(mdb.SettingGroupSystem, mdb.SettingKeyOrderExpiration, "10", mdb.SettingTypeInt); err != nil {
		t.Fatalf("set expiration: %v", err)
	}
	createdAt := time.Now().Add(-20 * time.Minute)
	seedBinanceDepositOrder(t, "trade-binance-timely", "TTimelyAddress", 50.01, mdb.StatusExpired, createdAt)
	seedBinanceDepositOrder(t, "trade-binance-late", "TLateAddress", 50.02, mdb.StatusExpired, createdAt)

	processed, err := ProcessBinanceDeposit("deposit-timely", mdb.NetworkTron, "TTimelyAddress", "USDT", 50.01, createdAt.Add(9*time.Minute).UnixMilli())
	if err != nil || !processed {
		t.Fatalf("timely expired payment = (%v, %v), want (true, nil)", processed, err)
	}
	processed, err = ProcessBinanceDeposit("deposit-late", mdb.NetworkTron, "TLateAddress", "USDT", 50.02, createdAt.Add(time.Duration(config.GetOrderExpirationTime()+1)*time.Minute).UnixMilli())
	if err != nil || processed {
		t.Fatalf("late expired payment = (%v, %v), want (false, nil)", processed, err)
	}
}

func TestProcessBinanceDepositIgnoresMismatchesAndPreOrderPayment(t *testing.T) {
	cleanup := testutil.SetupTestDatabases(t)
	defer cleanup()

	createdAt := time.Now().Add(-time.Minute)
	seedBinanceDepositOrder(t, "trade-binance-mismatch", "TExpectedAddress", 20.05, mdb.StatusWaitPay, createdAt)

	tests := []struct {
		id      string
		network string
		address string
		token   string
		amount  float64
		paidAt  int64
	}{
		{"wrong-address", mdb.NetworkTron, "TOtherAddress", "USDT", 20.05, createdAt.Add(time.Second).UnixMilli()},
		{"wrong-amount", mdb.NetworkTron, "TExpectedAddress", "USDT", 20.06, createdAt.Add(time.Second).UnixMilli()},
		{"wrong-token", mdb.NetworkTron, "TExpectedAddress", "USDC", 20.05, createdAt.Add(time.Second).UnixMilli()},
		{"pre-order", mdb.NetworkTron, "TExpectedAddress", "USDT", 20.05, createdAt.Add(-time.Second).UnixMilli()},
	}
	for _, tt := range tests {
		processed, err := ProcessBinanceDeposit(tt.id, tt.network, tt.address, tt.token, tt.amount, tt.paidAt)
		if err != nil || processed {
			t.Fatalf("%s = (%v, %v), want (false, nil)", tt.id, processed, err)
		}
	}
}
