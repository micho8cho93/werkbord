package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"time"
	"werkbord/internal/assistant/provider"
	"werkbord/internal/integration"
	"werkbord/internal/productmap"
	"werkbord/internal/store"
)

type mapScripted struct{}

func (mapScripted) ID() string { return "codex" }
func (mapScripted) Detect(context.Context) provider.Info {
	return provider.Info{ID: "codex", Name: "Codex (browser fixture)", Available: true}
}
func (mapScripted) Models(context.Context) provider.Models { return provider.Models{} }
func (mapScripted) Run(ctx context.Context, t provider.Turn, emit func(provider.Event)) (provider.Result, error) {
	select {
	case <-ctx.Done():
		return provider.Result{}, ctx.Err()
	case <-time.After(150 * time.Millisecond):
	}
	if t.Ref == "" {
		return provider.Result{Ref: "map-fixture", Text: `{"action":"read","path":"main.go"}`}, nil
	}
	c := integration.ProductMapContent{Features: []integration.ProductFeature{}, Relationships: []integration.ProductRelationship{{From: "orders", To: "checkout", Description: "Checkout creates an order"}, {From: "checkout", To: "receipts", Description: "Completed purchases receive a receipt"}}, Limitations: []string{"Synthetic browser fixture. No model was used."}}
	for _, f := range []struct{ ID, Name, Description string }{{"orders", "Order tracking", "Inspect order progress and delivery status."}, {"checkout", "Checkout", "Complete a purchase and create an order."}, {"receipts", "Receipts", "Find the receipt for a completed purchase."}} {
		c.Features = append(c.Features, integration.ProductFeature{ID: f.ID, Name: f.Name, Description: f.Description, Scenarios: []string{"Given a customer action, the supported result is recorded."}, Evidence: []integration.ProductEvidence{{Path: "main.go", Start: 1, End: 3}}})
	}
	raw, _ := json.Marshal(map[string]any{"action": "finish", "content": c})
	return provider.Result{Ref: "map-fixture", Text: string(raw), Model: "browser-fixture"}, nil
}
func newFixtureProductMaps(ctx context.Context, dir string, db store.Store) (*productmap.Engine, error) {
	reg := provider.NewRegistry()
	if err := reg.Register(mapScripted{}); err != nil {
		return nil, err
	}
	return productmap.New(ctx, productmap.Config{Store: db, Providers: reg, WorkDir: filepath.Join(dir, "product-maps")})
}
