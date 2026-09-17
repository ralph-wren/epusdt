package mdb

import (
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

func TestIndexedStringFieldsHaveBoundedMySQLLength(t *testing.T) {
	models := []interface{}{
		&Orders{}, &WalletAddress{}, &AdminUser{}, &ApiKey{}, &Setting{},
		&RateCache{}, &NotificationChannel{}, &Chain{}, &ChainToken{},
		&RpcNode{}, &ProviderOrder{}, &ProcessedTransaction{},
		&TransactionLock{}, &EvmScanCursor{},
	}

	for _, model := range models {
		parsed, err := schema.Parse(model, &sync.Map{}, schema.NamingStrategy{SingularTable: true})
		if err != nil {
			t.Fatalf("parse %T: %v", model, err)
		}
		for _, index := range parsed.ParseIndexes() {
			for _, option := range index.Fields {
				if option.Field != nil && option.Field.DataType == schema.String && option.Length == 0 && option.Field.Size <= 0 {
					t.Errorf("%s.%s is indexed by %s without a bounded length", parsed.Name, option.Field.Name, index.Name)
				}
			}
		}
	}
}

func TestSingleColumnUniqueFieldsUseColumnConstraints(t *testing.T) {
	tests := []struct {
		model interface{}
		field string
	}{
		{&Orders{}, "TradeId"},
		{&AdminUser{}, "Username"},
		{&ApiKey{}, "Pid"},
		{&Setting{}, "Key"},
		{&Chain{}, "Network"},
		{&ProcessedTransaction{}, "TradeID"},
		{&EvmScanCursor{}, "Network"},
	}

	for _, test := range tests {
		parsed, err := schema.Parse(test.model, &sync.Map{}, schema.NamingStrategy{SingularTable: true})
		if err != nil {
			t.Fatalf("parse %T: %v", test.model, err)
		}
		field := parsed.LookUpField(test.field)
		if field == nil {
			t.Fatalf("%s.%s was not parsed", parsed.Name, test.field)
		}
		if !field.Unique {
			t.Errorf("%s.%s must use a column-level unique constraint for MySQL AutoMigrate", parsed.Name, test.field)
		}
	}
}
