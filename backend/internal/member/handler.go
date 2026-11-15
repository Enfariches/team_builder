package member

import (
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

func (h *Handler) JoinSplit(ctx context.Context, req gen.JoinSplitRequestObject) (gen.JoinSplitResponseObject, error) {
	uid, err := h.userID(ctx)
	if err != nil {
		return nil, fmt.Errorf("member.Handler.JoinSplit: %w", err)
	}
	m, err := h.svc.Join(ctx, uid, req.Body.InviteToken, req.Body.FormId)
	if err != nil {
		return nil, fmt.Errorf("member.Handler.JoinSplit: %w", err)
	}
	return gen.JoinSplit201JSONResponse(toDTO(m)), nil
}

func (h *Handler) ListSplitMembers(ctx context.Context, req gen.ListSplitMembersRequestObject) (gen.ListSplitMembersResponseObject, error) {
	uid, err := h.userID(ctx)
	if err != nil {
		return nil, fmt.Errorf("member.Handler.ListSplitMembers: %w", err)
	}
	list, err := h.svc.ListBySplit(ctx, uid, req.SplitId)
	if err != nil {
		return nil, fmt.Errorf("member.Handler.ListSplitMembers: %w", err)
	}
	out := make(gen.ListSplitMembers200JSONResponse, len(list))
	for i, m := range list {
		out[i] = toDetailedDTO(m)
	}
	return out, nil
}

func (h *Handler) GetMyMembership(ctx context.Context, req gen.GetMyMembershipRequestObject) (gen.GetMyMembershipResponseObject, error) {
	uid, err := h.userID(ctx)
	if err != nil {
		return nil, fmt.Errorf("member.Handler.GetMyMembership: %w", err)
	}
	m, err := h.svc.GetMine(ctx, uid, req.SplitId)
	if err != nil {
		return nil, fmt.Errorf("member.Handler.GetMyMembership: %w", err)
	}
	return gen.GetMyMembership200JSONResponse(toDTO(m)), nil
}

func (h *Handler) LeaveSplit(ctx context.Context, req gen.LeaveSplitRequestObject) (gen.LeaveSplitResponseObject, error) {
	uid, err := h.userID(ctx)
	if err != nil {
		return nil, fmt.Errorf("member.Handler.LeaveSplit: %w", err)
	}
	if err := h.svc.Leave(ctx, uid, req.SplitId); err != nil {
		return nil, fmt.Errorf("member.Handler.LeaveSplit: %w", err)
	}
	return gen.LeaveSplit204Response{}, nil
}

func (h *Handler) RemoveMember(ctx context.Context, req gen.RemoveMemberRequestObject) (gen.RemoveMemberResponseObject, error) {
	uid, err := h.userID(ctx)
	if err != nil {
		return nil, fmt.Errorf("member.Handler.RemoveMember: %w", err)
	}
	if err := h.svc.Remove(ctx, uid, req.MemberId); err != nil {
		return nil, fmt.Errorf("member.Handler.RemoveMember: %w", err)
	}
	return gen.RemoveMember204Response{}, nil
}

func toDTO(m Member) gen.Member {
	role := gen.MemberRoleMember
	return gen.Member{
		Id: &m.ID, SplitId: &m.SplitID, UserId: &m.UserID, User: userDTO(m.User),
		Role: &role, FormId: m.FormID, TeamId: m.TeamID, AssignedRole: m.AssignedRole,
		JoinedAt: &m.JoinedAt,
	}
}

func toDetailedDTO(m Member) gen.MemberDetailed {
	role := gen.MemberRoleMember
	prefs := make([]gen.RolePreference, len(m.Snapshot.RolePreferences))
	for i, p := range m.Snapshot.RolePreferences {
		prefs[i] = gen.RolePreference{RoleName: p.RoleName, Rank: p.Rank}
	}
	skills := m.Snapshot.Skills
	if skills == nil {
		skills = []string{}
	}
	d := gen.MemberDetailed{
		Id: &m.ID, SplitId: &m.SplitID, UserId: &m.UserID, User: userDTO(m.User),
		Role: &role, FormId: m.FormID, TeamId: m.TeamID, AssignedRole: m.AssignedRole,
		JoinedAt: &m.JoinedAt,
	}
	d.FormSnapshot = &struct {
		Comment         *string               `json:"comment,omitempty"`
		Experience      *int                  `json:"experience,omitempty"`
		RolePreferences *[]gen.RolePreference `json:"rolePreferences,omitempty"`
		Skills          *[]string             `json:"skills,omitempty"`
	}{Comment: m.Snapshot.Comment, Experience: &m.Snapshot.Experience, RolePreferences: &prefs, Skills: &skills}
	return d
}

func userDTO(u UserBrief) *gen.UserBrief {
	return &gen.UserBrief{Id: &u.ID, FirstName: &u.FirstName, LastName: &u.LastName, Link: u.Link}
}
