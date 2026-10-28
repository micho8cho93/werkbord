package workspaces

import (
	"context"
	"errors"
	"net/http"
	"werkbord/internal/integration"
	"werkbord/internal/workspace"
)

func (p *Individual) ProductMapSnapshot(ctx context.Context, id string) (integration.ProductMapSnapshot, error) {
	var out integration.ProductMapSnapshot
	if !workspace.ValidID(id) {
		return out, errors.New("invalid project")
	}
	a, err := p.Access(ctx)
	if err != nil {
		return out, err
	}
	base, err := CheckBase(a.Base)
	if err != nil {
		return out, err
	}
	if a.Token == "" {
		return out, ErrNoCredential
	}
	err = p.send(ctx, base, a.Token, http.MethodGet, "/api/projects/"+id+"/product-map/snapshot", nil, &out)
	if err != nil {
		return out, err
	}
	return out, out.Validate()
}
