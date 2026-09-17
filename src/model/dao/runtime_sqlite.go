package dao

import (
	"errors"
	"strings"

	"github.com/GMWalletApp/epusdt/config"
	"github.com/GMWalletApp/epusdt/model/mdb"
	"github.com/gookit/color"
	"github.com/spf13/viper"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

var RuntimeDB *gorm.DB

func RuntimeInit() error {
	runtimeType := strings.ToLower(strings.TrimSpace(viper.GetString("runtime_db_type")))
	if runtimeType == "" {
		runtimeType = "sqlite"
	}

	var err error
	switch runtimeType {
	case "primary":
		if Mdb == nil {
			return errors.New("runtime_db_type=primary requires the primary database to be initialized")
		}
		RuntimeDB = Mdb
		color.Green.Printf("[runtime_db] using primary %s database\n", Mdb.Dialector.Name())
	case "sqlite":
		err = initRuntimeSQLite()
	default:
		return errors.New("runtime_db_type must be sqlite or primary")
	}
	if err != nil {
		return err
	}

	if RuntimeDB.Dialector.Name() == "sqlite" {
		if err = dropLegacyRuntimeSQLiteIndexes(); err != nil {
			return err
		}
	}
	if err = RuntimeDB.AutoMigrate(&mdb.TransactionLock{}); err != nil {
		color.Red.Printf("[runtime_db] migrate DB(TransactionLock),err=%s\n", err)
		return err
	}
	if err = RuntimeDB.AutoMigrate(&mdb.EvmScanCursor{}); err != nil {
		color.Red.Printf("[runtime_db] migrate DB(EvmScanCursor),err=%s\n", err)
		return err
	}

	return nil
}

func initRuntimeSQLite() error {
	runtimePath := config.GetRuntimeSqlitePath()
	color.Green.Printf("[runtime_db] sqlite filename: %s\n", runtimePath)

	var err error
	RuntimeDB, err = openDB(runtimePath, &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
		Logger:         logger.Default.LogMode(logger.Error),
	})
	if err != nil {
		color.Red.Printf("[runtime_db] sqlite open DB,err=%s\n", err)
		return err
	}

	concurrency := config.GetQueueConcurrency()
	if concurrency < 2 {
		concurrency = 2
	}
	if concurrency > 16 {
		concurrency = 16
	}
	if _, err = configureSQLite(RuntimeDB, concurrency); err != nil {
		color.Red.Printf("[runtime_db] sqlite connDB err:%s", err.Error())
		return err
	}
	return nil
}

func dropLegacyRuntimeSQLiteIndexes() error {
	for _, name := range []string{
		"transaction_lock_token_amount_uindex",
		"transaction_lock_address_token_amount_uindex",
		"transaction_lock_network_address_token_amount_uindex",
	} {
		if err := RuntimeDB.Exec("DROP INDEX IF EXISTS " + name).Error; err != nil {
			return err
		}
	}
	return nil
}
