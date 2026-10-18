package auth

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"builder/backend/internal/api/gen"
)

const (
	refreshCookieName = "refresh_token"
	refreshCookiePath = "/api/v1/auth"
)

type Handler struct {
	svc          *Service
	secureCookie bool
	// userAgent берётся из контекста запроса, см. WithUserAgent.
}

func NewHandler(svc *Service, secureCookie bool) *Handler {
	return &Handler{svc: svc, secureCookie: secureCookie}
}

func (h *Handler) Register(ctx context.Context, req gen.RegisterRequestObject) (gen.RegisterResponseObject, error) {
	b := req.Body
	s, err := h.svc.Register(ctx, RegisterInput{
		Login: b.Login, Password: b.Password,
		FirstName: b.FirstName, MiddleName: b.MiddleName,
		LastName: b.LastName, Link: b.Link,
	}, userAgent(ctx))
	if err != nil {
		return nil, fmt.Errorf("auth.Handler.Register: %w", err)
	}
	return gen.Register201JSONResponse{
		Body:    tokensDTO(s),
		Headers: gen.Register201ResponseHeaders{SetCookie: h.refreshCookie(s)},
	}, nil
}

func (h *Handler) Login(ctx context.Context, req gen.LoginRequestObject) (gen.LoginResponseObject, error) {
	s, err := h.svc.Login(ctx, req.Body.Login, req.Body.Password, userAgent(ctx))
	if err != nil {
		return nil, fmt.Errorf("auth.Handler.Login: %w", err)
	}
	return gen.Login200JSONResponse{
		Body:    tokensDTO(s),
		Headers: gen.Login200ResponseHeaders{SetCookie: h.refreshCookie(s)},
	}, nil
}

func (h *Handler) RefreshToken(ctx context.Context, req gen.RefreshTokenRequestObject) (gen.RefreshTokenResponseObject, error) {
	s, err := h.svc.Refresh(ctx, deref(req.Params.RefreshToken), userAgent(ctx))
	if err != nil {
		return nil, fmt.Errorf("auth.Handler.RefreshToken: %w", err)
	}
	return gen.RefreshToken200JSONResponse{
		Body:    tokensDTO(s),
		Headers: gen.RefreshToken200ResponseHeaders{SetCookie: h.refreshCookie(s)},
	}, nil
}

func (h *Handler) Logout(ctx context.Context, req gen.LogoutRequestObject) (gen.LogoutResponseObject, error) {
	if err := h.svc.Logout(ctx, deref(req.Params.RefreshToken)); err != nil {
		return nil, fmt.Errorf("auth.Handler.Logout: %w", err)
	}
	return gen.Logout204Response{Headers: gen.Logout204ResponseHeaders{SetCookie: h.clearCookie()}}, nil
}

func (h *Handler) refreshCookie(s Session) *string {
	c := &http.Cookie{
		Name: refreshCookieName, Value: s.RefreshToken, Path: refreshCookiePath,
		Expires: s.RefreshExpiresAt, HttpOnly: true, Secure: h.secureCookie,
		SameSite: http.SameSiteLaxMode,
	}
	v := c.String()
	return &v
}

func (h *Handler) clearCookie() *string {
	c := &http.Cookie{
		Name: refreshCookieName, Value: "", Path: refreshCookiePath,
		Expires: time.Unix(0, 0), MaxAge: -1, HttpOnly: true, Secure: h.secureCookie,
		SameSite: http.SameSiteLaxMode,
	}
	v := c.String()
	return &v
}

func tokensDTO(s Session) gen.AuthTokens {
	u := s.User
	return gen.AuthTokens{
		AccessToken: s.AccessToken,
		ExpiresIn:   int(s.ExpiresIn.Seconds()),
		User: gen.User{
			Id: &u.ID, Login: &u.Login, FirstName: &u.FirstName,
			MiddleName: u.MiddleName, LastName: &u.LastName,
			Link: u.Link, CreatedAt: &u.CreatedAt,
		},
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
