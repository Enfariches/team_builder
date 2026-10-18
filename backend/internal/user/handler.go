package user

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

func (h *Handler) GetCurrentUser(ctx context.Context, _ gen.GetCurrentUserRequestObject) (gen.GetCurrentUserResponseObject, error) {
	id, ok := h.identity.UserID(ctx)
	if !ok {
		return gen.GetCurrentUser401JSONResponse{UnauthorizedJSONResponse: unauthorized()}, nil
	}
	acc, err := h.svc.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("user.Handler.GetCurrentUser: get user %d: %w", id, err)
	}
	return gen.GetCurrentUser200JSONResponse(ToDTO(acc.User)), nil
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
