package workspaces

import (
	"context"
	"errors"
	"net/http"

	"werkbord/internal/integration"
	"werkbord/internal/workspace"
)

// ProjectTransfer uses only the owner's loopback controller and fixed semantic routes.
func (p *Individual) ProjectTransfer(ctx context.Context, operation, id string, in *integration.ProjectTransfer) (integration.ProjectTransfer, string, error) {
	var snapshot integration.ProjectTransfer
	var result struct {
		ID string `json:"id"`
	}
	a, err := p.Access(ctx)
	if err != nil {
		return snapshot, "", err
	}
	base, err := CheckBase(a.Base)
	if err != nil {
		return snapshot, "", err
	}
	if a.Token == "" {
		return snapshot, "", errors.New("owner credential is unavailable")
	}
	path := "/api/project-transfer"
	switch operation {
	case "export":
		if !workspace.ValidID(id) {
			return snapshot, "", errors.New("invalid project")
		}
		err = p.send(ctx, base, a.Token, http.MethodGet, path+"/"+id, nil, &snapshot)
	case "import":
		err = p.send(ctx, base, a.Token, http.MethodPost, path, in, &result)
	case "archive":
		if !workspace.ValidID(id) {
			return snapshot, "", errors.New("invalid project")
		}
		err = p.send(ctx, base, a.Token, http.MethodPost, path+"/"+id+"/archive", in, nil)
	default:
		err = errors.New("invalid transfer operation")
	}
	return snapshot, result.ID, err
}
