package service

import (
	"errors"
	"strings"
	"time"

	"github.com/GMWalletApp/epusdt/config"
	"github.com/GMWalletApp/epusdt/model/data"
	"github.com/GMWalletApp/epusdt/model/mdb"
	"github.com/GMWalletApp/epusdt/model/request"
	"github.com/GMWalletApp/epusdt/util/constant"
)

const binanceProcessedTransactionNetwork = "binance"

// ProcessBinanceDeposit matches a completed Binance deposit to an on-chain
// order. It returns true only when this call changes the order to paid.
func ProcessBinanceDeposit(depositID, network, address, token string, amount float64, paidAtMs int64) (bool, error) {
	depositID = strings.TrimSpace(depositID)
	network = strings.ToLower(strings.TrimSpace(network))
	address = normalizeOrderAddressByNetwork(network, address)
	token = strings.ToUpper(strings.TrimSpace(token))
	if depositID == "" || network == "" || address == "" || token == "" || amount <= 0 || paidAtMs <= 0 {
		return false, nil
	}

	paidAt := time.UnixMilli(paidAtMs)
	order, err := data.GetOnChainOrderByWalletAddressAndAmountAndTokenBeforeTime(network, address, token, amount, paidAt)
	if err != nil {
		return false, err
	}
	if order == nil || order.ID == 0 {
		return false, nil
	}

	expiresAtMs := order.CreatedAt.TimestampMilli() + int64(config.GetOrderExpirationTime())*int64(time.Minute/time.Millisecond)
	if paidAtMs > expiresAtMs {
		return false, nil
	}

	err = orderProcessing(&request.OrderProcessingRequest{
		ReceiveAddress:     order.ReceiveAddress,
		Currency:           order.Currency,
		Token:              order.Token,
		Network:            order.Network,
		Amount:             order.ActualAmount,
		TradeId:            order.TradeId,
		BlockTransactionId: "binance:" + depositID,
	}, orderProcessingOptions{
		allowedStatuses:       []int{mdb.StatusWaitPay, mdb.StatusExpired},
		parentAllowedStatuses: []int{mdb.StatusWaitPay, mdb.StatusExpired},
		claimNetwork:          binanceProcessedTransactionNetwork,
		claimTransactionID:    depositID,
	})
	if err != nil {
		if errors.Is(err, constant.OrderBlockAlreadyProcess) || errors.Is(err, constant.OrderStatusConflict) {
			return false, nil
		}
		return false, err
	}

	sendPaymentNotification(order)
	return true, nil
}
