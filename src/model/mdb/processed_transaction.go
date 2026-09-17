package mdb

import "strings"

// ProcessedTransaction is the durable claim for a provider or blockchain
// transaction. The database uniqueness constraint prevents two application
// instances from crediting different orders with the same transaction.
type ProcessedTransaction struct {
	Network            string `gorm:"column:network;size:32;uniqueIndex:processed_transactions_network_tx_uindex,priority:1"`
	BlockTransactionID string `gorm:"column:block_transaction_id;size:191;uniqueIndex:processed_transactions_network_tx_uindex,priority:2"`
	TradeID            string `gorm:"column:trade_id;size:64;uniqueIndex:processed_transactions_trade_id_uindex"`
	BaseModel
}

func (p *ProcessedTransaction) TableName() string {
	return "processed_transactions"
}

func NormalizeProcessedTransaction(network, transactionID string) (string, string) {
	network = strings.ToLower(strings.TrimSpace(network))
	transactionID = strings.TrimSpace(transactionID)
	switch network {
	case NetworkEthereum, NetworkBsc, NetworkPolygon, NetworkPlasma:
		transactionID = strings.ToLower(transactionID)
	}
	return network, transactionID
}
