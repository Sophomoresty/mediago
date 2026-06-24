package eoffcn

import "testing"

func TestParseParamsCapturesSpuID(t *testing.T) {
	p := parseParams(`https://www.eoffcn.com/goods/detail/abc123?spuId=SPU-88`)
	if p.SpuID != "SPU-88" {
		t.Fatalf("SpuID = %q, want %q", p.SpuID, "SPU-88")
	}
}

func TestCollectOldOrdersAndMatch(t *testing.T) {
	payload := map[string]any{
		"data": map[string]any{
			"list": []any{
				map[string]any{
					"payMoney":  199.5,
					"spuId":     "SPU-1",
					"systemSn":  "SYS-1",
					"cargoName": "旧课一",
				},
				map[string]any{
					"payMoney": 99,
					"spuId":    "SPU-2",
					"systemSn": "SYS-2",
					"orderInfoExpand": map[string]any{
						"systemSn": "SYS-2",
					},
					"goodsName": "旧课二",
				},
			},
		},
	}
	orders := collectOldOrders(payload)
	if len(orders) != 2 {
		t.Fatalf("orders = %d, want 2", len(orders))
	}
	got, ok := matchOldOrder(orders, eoffcnParams{SpuID: "SPU-2"})
	if !ok {
		t.Fatalf("match by spuId failed")
	}
	if got.SystemOrder != "SYS-2" || got.Title != "旧课二" {
		t.Fatalf("matched order = %#v, want SYS-2/旧课二", got)
	}
	got, ok = matchOldOrder(orders, eoffcnParams{SystemOrder: "SYS-1"})
	if !ok {
		t.Fatalf("match by system order failed")
	}
	if got.SpuID != "SPU-1" || got.PayMoney != "199.5" {
		t.Fatalf("matched order = %#v, want SPU-1/199.5", got)
	}
}
