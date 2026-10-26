package server

import (
	"werkbord/internal/entitlement"
	"werkbord/internal/team/config"
	"werkbord/internal/team/service"
)

// configurePlan holds a production workspace to the plan rules (docs/PLANS.md): a team of up to two people is free, and a
// team of more needs every member's Premium unlock key, checked against the public key this build is stamped with.
func configurePlan(svc *service.Service, cfg config.Config) {
	if cfg.Production && !cfg.Unmetered {
		svc.EnforcePlan(func(key string) (string, error) {
			c, err := entitlement.VerifyKey(key)
			return c.ID, err
		})
	}
}
