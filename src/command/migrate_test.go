package command

import (
	"path/filepath"
	"testing"

	"github.com/GMWalletApp/epusdt/model/mdb"
	"github.com/libtnb/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestMigrateSQLiteDatabasesCopiesPrimaryAndRuntimeRows(t *testing.T) {
	primary := openMigrationTestDB(t, filepath.Join(t.TempDir(), "primary.db"))
	runtimeDB := openMigrationTestDB(t, filepath.Join(t.TempDir(), "runtime.db"))
	target := openMigrationTestDB(t, filepath.Join(t.TempDir(), "target.db"))

	if err := primary.AutoMigrate(&mdb.Orders{}, &mdb.ApiKey{}); err != nil {
		t.Fatalf("migrate primary source: %v", err)
	}
	if err := runtimeDB.AutoMigrate(&mdb.TransactionLock{}, &mdb.EvmScanCursor{}); err != nil {
		t.Fatalf("migrate runtime source: %v", err)
	}
	if err := primary.Create(&mdb.ApiKey{Name: "merchant", Pid: "1001", SecretKey: "secret", Status: 1}).Error; err != nil {
		t.Fatalf("seed api key: %v", err)
	}
	if err := primary.Create(&mdb.Orders{
		TradeId: "trade-1", OrderId: "order-1", ApiKeyID: 1,
		Network: "ethereum", BlockTransactionId: "0xabc", Status: mdb.StatusPaySuccess,
	}).Error; err != nil {
		t.Fatalf("seed order: %v", err)
	}
	if err := runtimeDB.Create(&mdb.EvmScanCursor{Network: "ethereum", LastBlock: 123}).Error; err != nil {
		t.Fatalf("seed cursor: %v", err)
	}

	counts, err := migrateSQLiteDatabases(primary, runtimeDB, target)
	if err != nil {
		t.Fatalf("migrate SQLite databases: %v", err)
	}
	if counts["orders"] != 1 || counts["api_keys"] != 1 || counts["evm_scan_cursor"] != 1 {
		t.Fatalf("unexpected migration counts: %#v", counts)
	}
	var processed int64
	if err := target.Model(&mdb.ProcessedTransaction{}).Count(&processed).Error; err != nil {
		t.Fatalf("count processed transactions: %v", err)
	}
	if processed != 1 {
		t.Fatalf("processed transaction count = %d, want 1", processed)
	}
}

func openMigrationTestDB(t *testing.T, path string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
	})
	if err != nil {
		t.Fatalf("open migration test database: %v", err)
	}
	t.Cleanup(func() { closeMigrationDB(db) })
	return db
}
