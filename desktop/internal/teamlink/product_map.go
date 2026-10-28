package teamlink

import (
	"context"
	"errors"
	"net/http"
	"werkbord/internal/integration"
)

type ProductMapTarget struct {
	Repository string `json:"repository"`
	CanPropose bool   `json:"canPropose"`
	Records    []struct {
		State   string `json:"state"`
		Version int64  `json:"version"`
	} `json:"records"`
}

func (l *Link) ProductMapTarget(ctx context.Context, w, p string) (ProductMapTarget, error) {
	var out ProductMapTarget
	slot, ok := SlotOf(w)
	if !ok || !boardID.MatchString(p) {
		return out, errors.New("invalid Team project")
	}
	err := l.call(ctx, http.MethodGet, "/w/"+slot+"/api/team/v1/projects/"+p+"/product-map", nil, &out)
	return out, err
}
func (l *Link) ProposeProductMap(ctx context.Context, w, p, id string, expected int64, snapshot integration.ProductMapSnapshot) (string, error) {
	slot, ok := SlotOf(w)
	if !ok || !boardID.MatchString(p) || !integration.Identifier(id) {
		return "", errors.New("invalid map publication scope")
	}
	var out struct {
		ID string `json:"id"`
	}
	err := l.call(ctx, http.MethodPost, "/w/"+slot+"/api/team/v1/projects/"+p+"/product-map/proposals", map[string]any{"id": id, "expectedVersion": expected, "snapshot": snapshot}, &out)
	return out.ID, err
}
