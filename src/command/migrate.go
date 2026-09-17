package command

import (
	"errors"
	"fmt"
	"strings"

	"github.com/GMWalletApp/epusdt/config"
	"github.com/GMWalletApp/epusdt/model/mdb"
	"github.com/libtnb/sqlite"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

var (
	sourcePrimaryPath string
	sourceRuntimePath string
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "database migration commands",
}

var migrateSQLiteToMySQLCmd = &cobra.Command{
	Use:   "sqlite-to-mysql",
	Short: "copy an existing SQLite installation into an empty MySQL database",
	RunE: func(cmd *cobra.Command, args []string) error {
		config.Init()
		if !strings.EqualFold(strings.TrimSpace(viper.GetString("db_type")), "mysql") {
			return errors.New("target config db_type must be mysql")
		}
		if strings.TrimSpace(sourcePrimaryPath) == "" || strings.TrimSpace(sourceRuntimePath) == "" {
			return errors.New("both --source-primary and --source-runtime are required")
		}

		sourcePrimary, err := openMigrationSQLite(sourcePrimaryPath, viper.GetString("sqlite_table_prefix"))
		if err != nil {
			return fmt.Errorf("open source primary sqlite: %w", err)
		}
		defer closeMigrationDB(sourcePrimary)

		sourceRuntime, err := openMigrationSQLite(sourceRuntimePath, "")
		if err != nil {
			return fmt.Errorf("open source runtime sqlite: %w", err)
		}
		defer closeMigrationDB(sourceRuntime)

		target, err := gorm.Open(mysql.Open(config.MysqlDns), &gorm.Config{
			NamingStrategy: schema.NamingStrategy{
				TablePrefix:   viper.GetString("mysql_table_prefix"),
				SingularTable: true,
			},
			Logger: logger.Default.LogMode(logger.Error),
		})
		if err != nil {
			return fmt.Errorf("open target mysql: %w", err)
		}
		defer closeMigrationDB(target)

		counts, err := migrateSQLiteDatabases(sourcePrimary, sourceRuntime, target)
		if err != nil {
			return err
		}
		for table, count := range counts {
			fmt.Fprintf(cmd.OutOrStdout(), "%s: %d rows\n", table, count)
		}
		fmt.Fprintln(cmd.OutOrStdout(), "SQLite to MySQL migration completed")
		return nil
	},
}

func init() {
	migrateSQLiteToMySQLCmd.Flags().StringVar(&sourcePrimaryPath, "source-primary", "", "path to the existing primary SQLite database")
	migrateSQLiteToMySQLCmd.Flags().StringVar(&sourceRuntimePath, "source-runtime", "", "path to the existing runtime SQLite database")
	migrateCmd.AddCommand(migrateSQLiteToMySQLCmd)
	rootCmd.AddCommand(migrateCmd)
}

func openMigrationSQLite(path, prefix string) (*gorm.DB, error) {
	return gorm.Open(sqlite.Open(path), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{TablePrefix: prefix, SingularTable: true},
		Logger:         logger.Default.LogMode(logger.Error),
	})
}

func closeMigrationDB(db *gorm.DB) {
	if db == nil {
		return
	}
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
}

func migrateSQLiteDatabases(sourcePrimary, sourceRuntime, target *gorm.DB) (map[string]int64, error) {
	models := []interface{}{
		&mdb.Orders{}, &mdb.WalletAddress{}, &mdb.AdminUser{}, &mdb.ApiKey{},
		&mdb.Setting{}, &mdb.RateCache{}, &mdb.NotificationChannel{}, &mdb.Chain{},
		&mdb.ChainToken{}, &mdb.RpcNode{}, &mdb.ProviderOrder{},
		&mdb.ProcessedTransaction{}, &mdb.TransactionLock{}, &mdb.EvmScanCursor{},
	}
	if err := target.AutoMigrate(models...); err != nil {
		return nil, fmt.Errorf("migrate target schema: %w", err)
	}
	for _, model := range models {
		var count int64
		if err := target.Unscoped().Model(model).Count(&count).Error; err != nil {
			return nil, fmt.Errorf("check target table: %w", err)
		}
		if count != 0 {
			return nil, fmt.Errorf("target database is not empty")
		}
	}

	counts := make(map[string]int64, len(models))
	copyPrimary := []func() error{
		func() error { return copyMigrationTable[mdb.Orders](sourcePrimary, target, counts) },
		func() error { return copyMigrationTable[mdb.WalletAddress](sourcePrimary, target, counts) },
		func() error { return copyMigrationTable[mdb.AdminUser](sourcePrimary, target, counts) },
		func() error { return copyMigrationTable[mdb.ApiKey](sourcePrimary, target, counts) },
		func() error { return copyMigrationTable[mdb.Setting](sourcePrimary, target, counts) },
		func() error { return copyMigrationTable[mdb.RateCache](sourcePrimary, target, counts) },
		func() error { return copyMigrationTable[mdb.NotificationChannel](sourcePrimary, target, counts) },
		func() error { return copyMigrationTable[mdb.Chain](sourcePrimary, target, counts) },
		func() error { return copyMigrationTable[mdb.ChainToken](sourcePrimary, target, counts) },
		func() error { return copyMigrationTable[mdb.RpcNode](sourcePrimary, target, counts) },
		func() error { return copyMigrationTable[mdb.ProviderOrder](sourcePrimary, target, counts) },
		func() error { return copyMigrationTable[mdb.ProcessedTransaction](sourcePrimary, target, counts) },
	}
	for _, copyTable := range copyPrimary {
		if err := copyTable(); err != nil {
			return nil, err
		}
	}
	if err := copyMigrationTable[mdb.TransactionLock](sourceRuntime, target, counts); err != nil {
		return nil, err
	}
	if err := copyMigrationTable[mdb.EvmScanCursor](sourceRuntime, target, counts); err != nil {
		return nil, err
	}
	if err := backfillMigrationProcessedTransactions(target); err != nil {
		return nil, err
	}
	return counts, nil
}

func copyMigrationTable[T any](source, target *gorm.DB, counts map[string]int64) error {
	var model T
	table := tableNameForModel(target, &model)
	if !source.Migrator().HasTable(&model) {
		counts[table] = 0
		return nil
	}
	var rows []T
	if err := source.Unscoped().Find(&rows).Error; err != nil {
		return fmt.Errorf("read source table %s: %w", table, err)
	}
	if len(rows) > 0 {
		if err := target.CreateInBatches(&rows, 200).Error; err != nil {
			return fmt.Errorf("copy target table %s: %w", table, err)
		}
	}
	counts[table] = int64(len(rows))
	return nil
}

func tableNameForModel(db *gorm.DB, model interface{}) string {
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(model); err != nil {
		return "unknown"
	}
	return stmt.Schema.Table
}

func backfillMigrationProcessedTransactions(target *gorm.DB) error {
	var orders []mdb.Orders
	if err := target.Select("trade_id", "network", "block_transaction_id").
		Where("status = ? AND block_transaction_id <> ''", mdb.StatusPaySuccess).
		Order("id asc").Find(&orders).Error; err != nil {
		return fmt.Errorf("load successful orders for transaction backfill: %w", err)
	}
	for _, order := range orders {
		network, transactionID := mdb.NormalizeProcessedTransaction(order.Network, order.BlockTransactionId)
		row := mdb.ProcessedTransaction{
			Network:            network,
			BlockTransactionID: transactionID,
			TradeID:            order.TradeId,
		}
		if err := target.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
			return fmt.Errorf("backfill processed transaction for %s: %w", order.TradeId, err)
		}
	}
	return nil
}
