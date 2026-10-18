package auth

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	"builder/backend/internal/api/gen"
	"builder/backend/internal/platform"
)

type uaKey struct{}

func userAgent(ctx context.Context) string {
	ua, _ := ctx.Value(uaKey{}).(string)
	return ua
}

// StrictMiddleware проверяет access-токен на всех операциях, кроме public
// (операции со `security: []` в спеке), кладёт id пользователя и User-Agent
// в контекст запроса.
func StrictMiddleware(tokens *TokenIssuer, public map[string]bool) gen.StrictMiddlewareFunc {
	return func(next gen.StrictHandlerFunc, operationID string) gen.StrictHandlerFunc {
		return func(c *echo.Context, req any) (any, error) {
			ctx := context.WithValue(c.Request().Context(), uaKey{}, c.Request().UserAgent())
			if !public[operationID] {
				raw, ok := strings.CutPrefix(c.Request().Header.Get("Authorization"), "Bearer ")
				if !ok || raw == "" {
					return nil, fmt.Errorf("auth.StrictMiddleware %s: bearer token missing: %w", operationID, ErrUnauthorized)
				}
				id, err := tokens.Parse(raw)
				if err != nil {
					return nil, fmt.Errorf("auth.StrictMiddleware %s: parse access token: %w", operationID, err)
				}
				ctx = WithUserID(ctx, id)
			}
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c, req)
		}
	}
}

// HTTPErrors — маппинг доменных ошибок в HTTP для platform.NewErrorHandler.
var HTTPErrors = []platform.ErrorRule{
	{Target: ErrInvalidCredentials, Status: http.StatusUnauthorized, Code: "invalid_credentials", Message: "Неверный логин или пароль"},
	{Target: ErrUnauthorized, Status: http.StatusUnauthorized, Code: "unauthorized", Message: "Токен отсутствует, недействителен или истёк"},
	{Target: ErrLoginTaken, Status: http.StatusConflict, Code: "login_taken", Message: "Пользователь с таким логином уже существует"},
}
