package user

import (
	"context"
	"errors"
	"fmt"

	"github.com/oapi-codegen/nullable"

	"builder/backend/internal/api/gen"
)

type Handler struct {
	svc      *Service
	identity IdentityProvider
}

func NewHandler(svc *Service, identity IdentityProvider) *Handler {
	return &Handler{svc: svc, identity: identity}
}

func (h *Handler) GetCurrentUser(ctx context.Context, _ gen.GetCurrentUserRequestObject) (gen.GetCurrentUserResponseObject, error) {
	id, ok := h.identity.UserID(ctx)
	if !ok {
		return gen.GetCurrentUser401JSONResponse{UnauthorizedJSONResponse: unauthorized()}, nil
	}
	acc, err := h.svc.GetByID(ctx, id)
	if errors.Is(err, ErrNotFound) {
		// токен ещё жив, а пользователя уже нет
		return gen.GetCurrentUser401JSONResponse{UnauthorizedJSONResponse: unauthorized()}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("user.Handler.GetCurrentUser: get user %d: %w", id, err)
	}
	return gen.GetCurrentUser200JSONResponse(ToDTO(acc.User)), nil
}

func (h *Handler) UpdateCurrentUser(ctx context.Context, req gen.UpdateCurrentUserRequestObject) (gen.UpdateCurrentUserResponseObject, error) {
	id, ok := h.identity.UserID(ctx)
	if !ok {
		return gen.UpdateCurrentUser401JSONResponse{UnauthorizedJSONResponse: unauthorized()}, nil
	}
	u, err := h.svc.UpdateProfile(ctx, id, fromDTO(*req.Body))
	if errors.Is(err, ErrNotFound) {
		return gen.UpdateCurrentUser401JSONResponse{UnauthorizedJSONResponse: unauthorized()}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("user.Handler.UpdateCurrentUser: %w", err)
	}
	return gen.UpdateCurrentUser200JSONResponse(ToDTO(u)), nil
}

func fromDTO(b gen.UserUpdate) ProfileUpdate {
	return ProfileUpdate{
		FirstName:  b.FirstName,
		LastName:   b.LastName,
		MiddleName: optionalFrom(b.MiddleName),
		Link:       optionalFrom(b.Link),
	}
}

func optionalFrom(n nullable.Nullable[string]) Optional[string] {
	switch {
	case !n.IsSpecified():
		return Optional[string]{}
	case n.IsNull():
		return Optional[string]{Set: true}
	default:
		v := n.MustGet()
		return Optional[string]{Set: true, Value: &v}
	}
}

func ToDTO(u User) gen.User {
	return gen.User{
		Id:         &u.ID,
		Login:      &u.Login,
		FirstName:  &u.FirstName,
		MiddleName: u.MiddleName,
		LastName:   &u.LastName,
		Link:       u.Link,
		CreatedAt:  &u.CreatedAt,
	}
}

func unauthorized() gen.UnauthorizedJSONResponse {
	return gen.UnauthorizedJSONResponse{
		Code:    "unauthorized",
		Message: "Токен отсутствует, недействителен или истёк",
	}
}
