package teamlink

import (
	"context"
	"errors"
	"net/http"

	"devboard/internal/integration"
)

// ProjectTransfer is shell-only: no frame or assistant relay exposes it.
func (l *Link) ProjectTransfer(ctx context.Context, workspaceID, operation, id string, in *integration.ProjectTransfer) (integration.ProjectTransfer, string, error) {
	var snapshot integration.ProjectTransfer
	var result struct {
		ID string `json:"id"`
	}
	slot, ok := SlotOf(workspaceID)
	if !ok {
		return snapshot, "", errors.New("invalid Team")
	}
	path := "/w/" + slot + "/api/team/v1/project-transfer"
	var err error
	switch operation {
	case "export":
		if !boardID.MatchString(id) {
			return snapshot, "", errors.New("invalid project")
		}
		err = l.call(ctx, http.MethodGet, path+"/"+id, nil, &snapshot)
	case "import":
		err = l.call(ctx, http.MethodPost, path, in, &result)
	case "archive":
		if !boardID.MatchString(id) {
			return snapshot, "", errors.New("invalid project")
		}
		err = l.call(ctx, http.MethodPost, path+"/"+id+"/archive", in, nil)
	default:
		err = errors.New("invalid transfer operation")
	}
	return snapshot, result.ID, err
}
