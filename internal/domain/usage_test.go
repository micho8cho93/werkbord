package domain

import "testing"

func TestUsageNeverFabricatesPrecision(t *testing.T) {
	if e := (Usage{CostKind: "usage_only"}).Validate(); e != nil {
		t.Fatal(e)
	}
	zero := 0.0
	for _, u := range []Usage{{CostUSD: &zero}, {CostUSD: &zero, CostKind: "usage_only"}, {CostUSD: &zero, CostKind: "actual_api"}, {CostKind: "invented"}, {Acceptance: "maybe"}} {
		if u.Validate() == nil {
			t.Fatalf("invalid accounting admitted %+v", u)
		}
	}
	if e := (Usage{CostUSD: &zero, CostKind: "actual_api", Source: "provider receipt"}).Validate(); e != nil {
		t.Fatal(e)
	}
}
