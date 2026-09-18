package data

import (
	"strings"
	"time"

	"github.com/GMWalletApp/epusdt/model/dao"
	"github.com/GMWalletApp/epusdt/model/mdb"
	"gorm.io/gorm"
)

// OrderListFilter bundles every filter supported by the admin orders
// list page. Zero values are ignored so callers can pass only what the
// user actually filtered on.
type OrderListFilter struct {
	Status   int
	Network  string
	Token    string
	Address  string
	Keyword  string // matches trade_id / order_id / block_transaction_id
	StartAt  *time.Time
	EndAt    *time.Time
	Page     int
	PageSize int
	// ParentOnly restricts the result to top-level orders only
	// (parent_trade_id = ''). Sub-orders are excluded from the listing.
	ParentOnly bool
}

// ListOrders returns a paginated order slice plus the total count under
// the same filter (for the UI pagination bar).
func ListOrders(f OrderListFilter) ([]mdb.Orders, int64, error) {
	tx := buildOrderListQuery(f)
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	page := f.Page
	if page < 1 {
		page = 1
	}
	size := f.PageSize
	if size < 1 {
		size = 20
	}
	if size > 200 {
		size = 200
	}
	var rows []mdb.Orders
	err := tx.Select("orders.*").Order("orders.id DESC").
		Offset((page - 1) * size).Limit(size).
		Find(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	if err = HydrateSettledPaymentDetailsForOrders(rows); err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func buildOrderListQuery(f OrderListFilter) *gorm.DB {
	tx := dao.Mdb.Model(&mdb.Orders{}).
		Joins("LEFT JOIN orders AS paid_sub ON paid_sub.id = orders.pay_by_sub_id")
	if f.ParentOnly {
		tx = topLevelOrders(tx)
	}
	if f.Status > 0 {
		tx = tx.Where("orders.status = ?", f.Status)
	}
	if f.Network != "" {
		tx = tx.Where("COALESCE(NULLIF(paid_sub.network, ''), orders.network) = ?", strings.ToLower(f.Network))
	}
	if f.Token != "" {
		tx = tx.Where("COALESCE(NULLIF(paid_sub.token, ''), orders.token) = ?", strings.ToUpper(f.Token))
	}
	if f.Address != "" {
		tx = tx.Where("COALESCE(NULLIF(paid_sub.receive_address, ''), orders.receive_address) = ?", f.Address)
	}
	if f.Keyword != "" {
		kw := "%" + strings.TrimSpace(f.Keyword) + "%"
		tx = tx.Where(`orders.trade_id LIKE ? OR orders.order_id LIKE ? OR
            orders.block_transaction_id LIKE ? OR paid_sub.trade_id LIKE ? OR paid_sub.block_transaction_id LIKE ?`,
			kw, kw, kw, kw, kw)
	}
	if f.StartAt != nil {
		tx = tx.Where("orders.created_at >= ?", *f.StartAt)
	}
	if f.EndAt != nil {
		tx = tx.Where("orders.created_at <= ?", *f.EndAt)
	}
	return tx
}

// topLevelOrders limits business-facing order views and aggregates to merchant
// orders. Network/token switch child orders remain available for audit, but
// must not be counted as a second payment for the same merchant order.
func topLevelOrders(tx *gorm.DB) *gorm.DB {
	return tx.Where("(orders.parent_trade_id = ? OR orders.parent_trade_id IS NULL)", "")
}

// HydrateSettledPaymentDetails replaces the payment-facing fields of a parent
// order with the child order that actually settled it. The stored parent row is
// intentionally left untouched so its original payment target remains
// available for audit.
func HydrateSettledPaymentDetails(order *mdb.Orders) error {
	if order == nil || order.PayBySubId == 0 {
		return nil
	}
	rows := []mdb.Orders{*order}
	if err := HydrateSettledPaymentDetailsForOrders(rows); err != nil {
		return err
	}
	*order = rows[0]
	return nil
}

// HydrateSettledPaymentDetailsForOrders is the batch variant used by admin
// lists and dashboard recent orders to avoid one child-order query per row.
func HydrateSettledPaymentDetailsForOrders(orders []mdb.Orders) error {
	ids := make([]uint64, 0, len(orders))
	seen := make(map[uint64]struct{}, len(orders))
	for i := range orders {
		id := orders[i].PayBySubId
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil
	}

	var settled []mdb.Orders
	if err := dao.Mdb.Where("id IN ?", ids).Find(&settled).Error; err != nil {
		return err
	}
	byID := make(map[uint64]mdb.Orders, len(settled))
	for _, row := range settled {
		byID[row.ID] = row
	}
	for i := range orders {
		paid, ok := byID[orders[i].PayBySubId]
		if !ok {
			continue
		}
		orders[i].ActualAmount = paid.ActualAmount
		orders[i].QuoteRate = paid.QuoteRate
		orders[i].ReceiveAddress = paid.ReceiveAddress
		orders[i].Token = paid.Token
		orders[i].Network = paid.Network
		orders[i].BlockTransactionId = paid.BlockTransactionId
		orders[i].PayProvider = paid.PayProvider
	}
	return nil
}

// CountOrdersByStatus returns how many orders exist in each status.
// Used by the dashboard overview card.
func CountOrdersByStatus() (map[int]int64, error) {
	type row struct {
		Status int
		Total  int64
	}
	var rows []row
	err := topLevelOrders(dao.Mdb.Model(&mdb.Orders{})).
		Select("orders.status, COUNT(*) AS total").
		Group("orders.status").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := map[int]int64{}
	for _, r := range rows {
		out[r.Status] = r.Total
	}
	return out, nil
}

// CloseOrderManually transitions a pending order to expired. It covers both
// payable orders and placeholder orders waiting for token/network selection.
func CloseOrderManually(tradeID string) (bool, error) {
	result := dao.Mdb.Model(&mdb.Orders{}).
		Where("trade_id = ?", tradeID).
		Where("status IN ?", []int{mdb.StatusWaitPay, mdb.StatusWaitSelect}).
		Update("status", mdb.StatusExpired)
	return result.RowsAffected > 0, result.Error
}

// ReopenOrderCallback flips callback_confirm back to NO so the mq
// worker picks it up on the next tick. Used by "resend callback".
func ReopenOrderCallback(tradeID string) (bool, error) {
	result := dao.Mdb.Model(&mdb.Orders{}).
		Where("trade_id = ?", tradeID).
		Where("status = ?", mdb.StatusPaySuccess).
		Updates(map[string]interface{}{
			"callback_confirm": mdb.CallBackConfirmNo,
			"callback_num":     0,
		})
	return result.RowsAffected > 0, result.Error
}

// CountOrdersByAddress returns order counts grouped by receive_address.
// The admin wallet list annotates each wallet row with this number.
func CountOrdersByAddress() (map[string]int64, error) {
	type row struct {
		Address string `gorm:"column:receive_address"`
		Total   int64
	}
	var rows []row
	err := dao.Mdb.Model(&mdb.Orders{}).
		Select("receive_address, COUNT(*) AS total").
		Group("receive_address").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := map[string]int64{}
	for _, r := range rows {
		out[r.Address] = r.Total
	}
	return out, nil
}
