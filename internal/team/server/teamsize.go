package server

import (
	"devboard/internal/entitlement"
	"devboard/internal/team/config"
	"devboard/internal/team/service"
)

// configureTeamSize holds a production workspace to its plan's size: the Free plan's two people unless cfg.Premium.
func configureTeamSize(svc *service.Service, cfg config.Config) {
	if cfg.Production {
		svc.EnforceTeamSize(entitlement.TeamLimit(cfg.Premium))
	}
}
