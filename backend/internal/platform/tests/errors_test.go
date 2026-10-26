package tests

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"

	"builder/backend/internal/platform"
)

var (
	errDomain = errors.New("domain")
	errOther  = errors.New("other")
)

type fieldsErr struct{}

func (fieldsErr) Error() string               { return "fields" }
func (fieldsErr) Fields() [][2]string         { return [][2]string{{"login", "коротко"}} }
func (fieldsErr) ErrorCode() (string, string) { return "my_code", "Моё сообщение" }

type msgErr struct{}

func (msgErr) Error() string       { return "msg" }
func (msgErr) Unwrap() error       { return errDomain }
func (msgErr) UserMessage() string { return "Динамическое сообщение" }

func TestErrorHandler(t *testing.T) {
	e := echo.New()
	e.HTTPErrorHandler = platform.NewErrorHandler(slog.Default(),
		platform.ErrorRule{Target: errDomain, Status: http.StatusConflict, Code: "domain_conflict", Message: "Конфликт"})
	e.GET("/domain", func(*echo.Context) error { return fmt.Errorf("wrapped: %w", errDomain) })
	e.GET("/dynamic", func(*echo.Context) error { return msgErr{} })
	e.GET("/fields", func(*echo.Context) error { return fieldsErr{} })
	e.GET("/unknown", func(*echo.Context) error { return errOther })

	tests := []struct {
		name, method, path string
		wantStatus         int
		wantContains       string
	}{
		{"доменная ошибка по правилу", "GET", "/domain", 409, `"code":"domain_conflict"`},
		{"динамическое сообщение", "GET", "/dynamic", 409, `"message":"Динамическое сообщение"`},
		{"ошибка валидации с кодом", "GET", "/fields", 422, `"code":"my_code"`},
		{"details валидации", "GET", "/fields", 422, `"field":"login"`},
		{"неизвестная ошибка — 500", "GET", "/unknown", 500, `"code":"internal_error"`},
		{"неизвестный маршрут — 404", "GET", "/nope", 404, `"code":"not_found"`},
		{"неверный метод — 405", "POST", "/domain", 405, `"code":"method_not_allowed"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))
			if rec.Code != tt.wantStatus || !strings.Contains(rec.Body.String(), tt.wantContains) {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
			}
		})
	}
}
