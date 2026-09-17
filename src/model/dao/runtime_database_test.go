package dao

import (
	"path/filepath"
	"testing"

	"github.com/GMWalletApp/epusdt/model/mdb"
	"github.com/libtnb/sqlite"
	"github.com/spf13/viper"
	"gorm.io/gorm"
)

func TestRuntimeInitCanReusePrimaryDatabase(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("runtime_db_type", "primary")

	primary, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "primary.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open primary database: %v", err)
	}
	Mdb = primary
	RuntimeDB = nil
	t.Cleanup(func() {
		sqlDB, dbErr := primary.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
		Mdb = nil
		RuntimeDB = nil
	})

	if err := RuntimeInit(); err != nil {
		t.Fatalf("RuntimeInit: %v", err)
	}
	if RuntimeDB != Mdb {
		t.Fatal("runtime database did not reuse the primary connection")
	}
	if !RuntimeDB.Migrator().HasTable(&mdb.TransactionLock{}) {
		t.Fatal("transaction_lock table was not migrated")
	}
	if !RuntimeDB.Migrator().HasTable(&mdb.EvmScanCursor{}) {
		t.Fatal("evm_scan_cursor table was not migrated")
	}
}

func TestRuntimeInitRejectsUnknownDatabaseType(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("runtime_db_type", "mysql")

	if err := RuntimeInit(); err == nil {
		t.Fatal("RuntimeInit returned nil for unsupported runtime_db_type")
	}
}
