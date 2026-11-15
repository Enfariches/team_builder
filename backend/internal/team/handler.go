package team

import (
	"bytes"
	"context"
	"fmt"

	"builder/backend/internal/api/gen"
)

type Handler struct {
	svc      *Service
	identity IdentityProvider
}

func NewHandler(svc *Service, identity IdentityProvider) *Handler {
	return &Handler{svc: svc, identity: identity}
}

// userID: middleware auth уже отсёк запросы без токена, поэтому отсутствие
// id в контексте означает ошибку сборки, а не клиента.
func (h *Handler) userID(ctx context.Context) (int64, error) {
	id, ok := h.identity.UserID(ctx)
	if !ok {
		return 0, fmt.Errorf("no authenticated user in context")
	}
	return id, nil
}

func (h *Handler) ListSplitTeams(ctx context.Context, req gen.ListSplitTeamsRequestObject) (gen.ListSplitTeamsResponseObject, error) {
	uid, err := h.userID(ctx)
	if err != nil {
		return nil, fmt.Errorf("team.Handler.ListSplitTeams: %w", err)
	}
	r, err := h.svc.List(ctx, uid, req.SplitId)
	if err != nil {
		return nil, fmt.Errorf("team.Handler.ListSplitTeams: %w", err)
	}
	out := make(gen.ListSplitTeams200JSONResponse, len(r.Teams))
	for i, t := range r.Teams {
		out[i] = teamDTO(t)
	}
	return out, nil
}

func (h *Handler) GetMyTeam(ctx context.Context, req gen.GetMyTeamRequestObject) (gen.GetMyTeamResponseObject, error) {
	uid, err := h.userID(ctx)
	if err != nil {
		return nil, fmt.Errorf("team.Handler.GetMyTeam: %w", err)
	}
	t, err := h.svc.My(ctx, uid, req.SplitId)
	if err != nil {
		return nil, fmt.Errorf("team.Handler.GetMyTeam: %w", err)
	}
	return gen.GetMyTeam200JSONResponse(teamDTO(t)), nil
}

func (h *Handler) ExportTeams(ctx context.Context, req gen.ExportTeamsRequestObject) (gen.ExportTeamsResponseObject, error) {
	uid, err := h.userID(ctx)
	if err != nil {
		return nil, fmt.Errorf("team.Handler.ExportTeams: %w", err)
	}
	data, err := h.svc.Export(ctx, uid, req.SplitId)
	if err != nil {
		return nil, fmt.Errorf("team.Handler.ExportTeams: %w", err)
	}
	return gen.ExportTeams200TextcsvResponse{Body: bytes.NewReader(data), ContentLength: int64(len(data))}, nil
}

func teamDTO(t Team) gen.Team {
	members := make([]gen.TeamMember, len(t.Members))
	for i, m := range t.Members {
		members[i] = gen.TeamMember{
			MemberId: &m.MemberID, AssignedRole: m.Role,
			User: &gen.UserBrief{Id: &m.User.ID, FirstName: &m.User.FirstName, LastName: &m.User.LastName, Link: m.User.Link},
		}
	}
	return gen.Team{Id: &t.ID, SplitId: &t.SplitID, Name: &t.Name, Members: &members, CreatedAt: &t.CreatedAt}
}
