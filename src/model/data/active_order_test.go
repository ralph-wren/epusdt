package data

import (
	"testing"
	"time"

	"github.com/GMWalletApp/epusdt/internal/testutil"
	"github.com/GMWalletApp/epusdt/model/dao"
	"github.com/GMWalletApp/epusdt/model/mdb"
)

func TestActiveOnChainOrdersGate(t *testing.T) {
	cleanup := testutil.SetupTestDatabases(t)
	defer cleanup()

	check := func(network string, want bool) {
		t.Helper()
		got, err := HasActiveOnChainOrders(network)
		if err != nil || got != want {
			t.Fatalf("HasActiveOnChainOrders(%q) = %v, %v; want %v", network, got, err, want)
		}
	}
	binance := func(want bool) {
		t.Helper()
		got, err := HasActiveBinanceDepositOrders()
		if err != nil || got != want {
			t.Fatalf("HasActiveBinanceDepositOrders() = %v, %v; want %v", got, err, want)
		}
	}
	seed := func(id, network, token, provider string, status int, age time.Duration) {
		t.Helper()
		order := &mdb.Orders{TradeId: id, OrderId: id, Network: network, Token: token, PayProvider: provider, Status: status}
		if err := dao.Mdb.Create(order).Error; err != nil {
			t.Fatal(err)
		}
		if age > 0 {
			if err := dao.Mdb.Model(order).Update("created_at", time.Now().Add(-age)).Error; err != nil {
				t.Fatal(err)
			}
		}
	}

	check(mdb.NetworkBsc, false)
	binance(false)
	seed("paid", mdb.NetworkBsc, "USDT", mdb.PaymentProviderOnChain, mdb.StatusPaySuccess, 0)
	seed("expired", mdb.NetworkBsc, "USDT", mdb.PaymentProviderOnChain, mdb.StatusExpired, 0)
	seed("hosted", mdb.NetworkBsc, "USDT", mdb.PaymentProviderOkPay, mdb.StatusWaitPay, 0)
	seed("stale", mdb.NetworkBsc, "USDT", mdb.PaymentProviderOnChain, mdb.StatusWaitPay, time.Hour)
	check(mdb.NetworkBsc, false)
	binance(false)
	seed("other-token", mdb.NetworkBsc, "USDC", mdb.PaymentProviderOnChain, mdb.StatusWaitPay, 0)
	check(mdb.NetworkBsc, true)
	binance(false)
	seed("legacy", mdb.NetworkTron, "USDT", "", mdb.StatusWaitPay, 0)
	check(mdb.NetworkTron, true)
	binance(true)
	check(mdb.NetworkEthereum, false)
	if err := dao.Mdb.Model(&mdb.Orders{}).Where("trade_id = ?", "legacy").Update("status", mdb.StatusPaySuccess).Error; err != nil {
		t.Fatal(err)
	}
	check(mdb.NetworkTron, false)
	binance(false)
}

func TestBinanceDepositOrderNotification(t *testing.T) {
	wakeup := BinanceDepositMonitorWakeup()
	select {
	case <-wakeup:
	default:
	}
	for _, order := range []*mdb.Orders{
		nil,
		{Status: mdb.StatusExpired, Token: "USDT", PayProvider: mdb.PaymentProviderOnChain},
		{Status: mdb.StatusWaitPay, Token: "USDC", PayProvider: mdb.PaymentProviderOnChain},
		{Status: mdb.StatusWaitPay, Token: "USDT", PayProvider: mdb.PaymentProviderOkPay},
	} {
		NotifyBinanceDepositOrderCreated(order)
	}
	select {
	case <-wakeup:
		t.Fatal("irrelevant order woke Binance deposit listener")
	default:
	}
	NotifyBinanceDepositOrderCreated(&mdb.Orders{Status: mdb.StatusWaitPay, Token: "usdt", PayProvider: mdb.PaymentProviderOnChain})
	NotifyBinanceDepositOrderCreated(&mdb.Orders{Status: mdb.StatusWaitPay, Token: "USDT", PayProvider: mdb.PaymentProviderOnChain})
	select {
	case <-wakeup:
	default:
		t.Fatal("eligible order did not wake Binance deposit listener")
	}
	select {
	case <-wakeup:
		t.Fatal("notifications should coalesce")
	default:
	}
}
