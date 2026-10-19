package server

import (
	"context"
	"devboard/internal/team/service"
	"log/slog"
	"time"
)

// Reads independently calculate freshness, so failed ticks and unavailable
// quorum affect audit upkeep only. Shutdown cancels and joins the worker.
func runMemoryReconciliation(ctx context.Context, s *service.Service, log *slog.Logger) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		tick, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := s.ReconcileMemory(tick)
		cancel()
		if err != nil && ctx.Err() == nil {
			log.Debug("memory reconciliation will retry", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
