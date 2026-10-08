package server

import (
	"io"
	"os"

	"devboard/internal/team/config"
	"devboard/internal/team/service"
)

func configureLicense(svc *service.Service, cfg config.Config) {
	if cfg.LicenseRequired {
		var raw []byte
		if f, err := os.Open(cfg.LicensePath()); err == nil {
			raw, _ = io.ReadAll(io.LimitReader(f, 16385))
			_ = f.Close()
		}
		svc.EnforceLicense(cfg.LicenseKey, raw)
	}
}
