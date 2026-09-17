package data

import (
	"testing"

	"github.com/GMWalletApp/epusdt/internal/testutil"
	"github.com/GMWalletApp/epusdt/model/dao"
	"github.com/GMWalletApp/epusdt/model/mdb"
)

func TestProcessedTransactionCanOnlyBeClaimedOnce(t *testing.T) {
	cleanup := testutil.SetupTestDatabases(t)
	defer cleanup()

	tx := dao.Mdb.Begin()
	claimed, err := ReserveProcessedTransactionWithTransaction(tx, "ethereum", "0xabc", "trade-1")
	if err != nil || !claimed {
		t.Fatalf("first claim = %v, %v; want true, nil", claimed, err)
	}
	if err = tx.Commit().Error; err != nil {
		t.Fatalf("commit first claim: %v", err)
	}

	tx = dao.Mdb.Begin()
	claimed, err = ReserveProcessedTransactionWithTransaction(tx, " Ethereum ", " 0xAbC ", "trade-2")
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if claimed {
		t.Fatal("duplicate transaction was claimed twice")
	}
	tx.Rollback()
}

func TestMerchantOrderIDIsScopedToApiKey(t *testing.T) {
	cleanup := testutil.SetupTestDatabases(t)
	defer cleanup()

	first := &mdb.Orders{TradeId: "trade-key-1", OrderId: "merchant-order", ApiKeyID: 1}
	second := &mdb.Orders{TradeId: "trade-key-2", OrderId: "merchant-order", ApiKeyID: 2}
	if err := dao.Mdb.Create(first).Error; err != nil {
		t.Fatalf("create first merchant order: %v", err)
	}
	if err := dao.Mdb.Create(second).Error; err != nil {
		t.Fatalf("create second merchant order: %v", err)
	}
	duplicate := &mdb.Orders{TradeId: "trade-key-1-duplicate", OrderId: "merchant-order", ApiKeyID: 1}
	if err := dao.Mdb.Create(duplicate).Error; err == nil {
		t.Fatal("same API key created duplicate merchant order id")
	}
}
