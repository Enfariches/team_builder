package invite

import (
	"context"
	"fmt"
	"strings"

	"builder/backend/internal/api/gen"
)

type Handler struct {
	svc      *Service
	identity IdentityProvider
	baseURL  string
}

// NewHandler: baseURL — адрес фронтенда, из него собирается ссылка для рассылки.
func NewHandler(svc *Service, identity IdentityProvider, baseURL string) *Handler {
	return &Handler{svc: svc, identity: identity, baseURL: strings.TrimRight(baseURL, "/")}
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

func (h *Handler) ListInvites(ctx context.Context, req gen.ListInvitesRequestObject) (gen.ListInvitesResponseObject, error) {
	uid, err := h.userID(ctx)
	if err != nil {
		return nil, fmt.Errorf("invite.Handler.ListInvites: %w", err)
	}
	list, err := h.svc.List(ctx, uid, req.SplitId)
	if err != nil {
		return nil, fmt.Errorf("invite.Handler.ListInvites: %w", err)
	}
	out := make(gen.ListInvites200JSONResponse, len(list))
	for i, inv := range list {
		out[i] = h.toDTO(inv)
	}
	return out, nil
}

func (h *Handler) CreateInvite(ctx context.Context, req gen.CreateInviteRequestObject) (gen.CreateInviteResponseObject, error) {
	uid, err := h.userID(ctx)
	if err != nil {
		return nil, fmt.Errorf("invite.Handler.CreateInvite: %w", err)
	}
	// тело необязательно: без него инвайт бессрочный и без лимита
	var n NewInvite
	if req.Body != nil {
		n = NewInvite{ExpiresAt: req.Body.ExpiresAt, MaxUses: req.Body.MaxUses}
	}
	inv, err := h.svc.Create(ctx, uid, req.SplitId, n)
	if err != nil {
		return nil, fmt.Errorf("invite.Handler.CreateInvite: %w", err)
	}
	return gen.CreateInvite201JSONResponse(h.toDTO(inv)), nil
}

func (h *Handler) RevokeInvite(ctx context.Context, req gen.RevokeInviteRequestObject) (gen.RevokeInviteResponseObject, error) {
	uid, err := h.userID(ctx)
	if err != nil {
		return nil, fmt.Errorf("invite.Handler.RevokeInvite: %w", err)
	}
	if err := h.svc.Revoke(ctx, uid, req.SplitId, req.InviteId); err != nil {
		return nil, fmt.Errorf("invite.Handler.RevokeInvite: %w", err)
	}
	return gen.RevokeInvite204Response{}, nil
}

func (h *Handler) PreviewInvite(ctx context.Context, req gen.PreviewInviteRequestObject) (gen.PreviewInviteResponseObject, error) {
	info, err := h.svc.Preview(ctx, req.Token)
	if err != nil {
		return nil, fmt.Errorf("invite.Handler.PreviewInvite: %w", err)
	}
	names := info.RoleNames
	if names == nil {
		names = []string{}
	}
	return gen.PreviewInvite200JSONResponse(gen.InvitePreview{
		SplitTitle:       info.Title,
		SplitDescription: info.Description,
		TeamSize:         info.TeamSize,
		RoleNames:        names,
		MembersCount:     info.MembersCount,
		Status:           gen.SplitStatus(info.Status),
	}), nil
}

func (h *Handler) toDTO(inv Invite) gen.Invite {
	return gen.Invite{
		Id:        inv.ID,
		Token:     inv.Token,
		Url:       h.baseURL + "/join/" + inv.Token,
		ExpiresAt: inv.ExpiresAt,
		MaxUses:   inv.MaxUses,
		UsesCount: inv.UsesCount,
		RevokedAt: inv.RevokedAt,
		CreatedAt: inv.CreatedAt,
	}
}
