package admin

import (
	"encoding/json"
	"testing"

	"github.com/GMWalletApp/epusdt/model/mdb"
	"github.com/dromara/carbon/v2"
)

func TestAdminOrderDatesJSON(t *testing.T) {
	order := mdb.Orders{
		TradeId: "example-trade",
		BaseModel: mdb.BaseModel{
			CreatedAt: *carbon.NewTime(carbon.Parse("2026-09-25 18:27:40")),
			UpdatedAt: *carbon.NewTime(carbon.Parse("2026-09-25 18:37:40")),
		},
	}

	for name, value := range map[string]any{
		"list":     OrderListResponse{List: withOrderDatesList([]mdb.Orders{order})},
		"detail":   withOrderDates(order),
		"with_sub": OrderWithSub{OrderWithDates: withOrderDates(order), SubOrders: withOrderDatesList([]mdb.Orders{order})},
	} {
		t.Run(name, func(t *testing.T) {
			encoded, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			var decoded any
			if err := json.Unmarshal(encoded, &decoded); err != nil {
				t.Fatal(err)
			}
			var item map[string]any
			switch name {
			case "list":
				item = decoded.(map[string]any)["list"].([]any)[0].(map[string]any)
			case "with_sub":
				item = decoded.(map[string]any)
				child := item["sub_orders"].([]any)[0].(map[string]any)
				if child["created_at"] != "2026-09-25 18:27:40" {
					t.Fatalf("sub-order date: %v", child["created_at"])
				}
			default:
				item = decoded.(map[string]any)
			}
			if item["trade_id"] != "example-trade" || item["created_at"] != "2026-09-25 18:27:40" || item["updated_at"] != "2026-09-25 18:37:40" {
				t.Fatalf("unexpected order JSON: %s", encoded)
			}
		})
	}
}
