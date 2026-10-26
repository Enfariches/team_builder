package tests

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"

	"builder/backend/internal/api/gen"
	"builder/backend/internal/platform"
	"builder/backend/internal/user"
)

type fakeIdentity struct {
	id int64
	ok bool
}

func (f fakeIdentity) UserID(context.Context) (int64, bool) { return f.id, f.ok }

type userAPI = user.Handler

// Вложенная структура сдвигает методы nil-интерфейса на уровень глубже,
// поэтому методы user.Handler побеждают без неоднозначности.
type unimplemented struct{ gen.StrictServerInterface }

// srv достраивает strict-интерфейс до полного: нужны только методы user.
type srv struct {
	unimplemented
	*userAPI
}

func newServer(st *fakeStore, id fakeIdentity) *echo.Echo {
	e := echo.New()
	e.HTTPErrorHandler = platform.NewErrorHandler(slog.Default(), user.HTTPErrors...)
	h := user.NewHandler(user.NewService(st), id)
	gen.RegisterHandlers(e, gen.NewStrictHandler(srv{userAPI: h}, nil))
	return e
}

func do(e *echo.Echo, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestPatchCurrentUser(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantCalls  int
		check      func(t *testing.T, u user.ProfileUpdate)
	}{
		{"поля нет", `{"firstName":"Игорь"}`, 200, 1, func(t *testing.T, u user.ProfileUpdate) {
			if u.Link.Set || u.MiddleName.Set {
				t.Fatalf("link/middleName не переданы, но Set: %+v", u)
			}
		}},
		{"link null очищает", `{"link":null}`, 200, 1, func(t *testing.T, u user.ProfileUpdate) {
			if !u.Link.Set || u.Link.Value != nil {
				t.Fatalf("Link = %+v, want Set && nil", u.Link)
			}
		}},
		{"link строка", `{"link":" @tg "}`, 200, 1, func(t *testing.T, u user.ProfileUpdate) {
			if !u.Link.Set || u.Link.Value == nil || *u.Link.Value != "@tg" {
				t.Fatalf("Link = %+v", u.Link)
			}
		}},
		{"пустое тело", `{}`, 200, 0, nil},
		{"пустое имя", `{"firstName":""}`, 422, 0, nil},
		{"битый JSON", `{`, 400, 0, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newFakeStore()
			rec := do(newServer(st, fakeIdentity{1, true}), http.MethodPatch, "/users/me", tt.body)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d, body: %s", rec.Code, tt.wantStatus, rec.Body)
			}
			if st.updateCalls != tt.wantCalls {
				t.Fatalf("Update вызван %d раз, want %d", st.updateCalls, tt.wantCalls)
			}
			if tt.check != nil {
				tt.check(t, st.lastUpdate)
			}
		})
	}
}

func TestPatchValidationDetails(t *testing.T) {
	rec := do(newServer(newFakeStore(), fakeIdentity{1, true}), http.MethodPatch, "/users/me", `{"firstName":"","lastName":""}`)
	var body struct {
		Code    string `json:"code"`
		Details []struct {
			Field string `json:"field"`
		} `json:"details"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 422 || body.Code != "validation_failed" || len(body.Details) != 2 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
	}
}

func TestCurrentUserUnauthorized(t *testing.T) {
	tests := []struct {
		name     string
		identity fakeIdentity
		method   string
		body     string
	}{
		{"GET без identity", fakeIdentity{}, http.MethodGet, ""},
		{"PATCH без identity", fakeIdentity{}, http.MethodPatch, `{"firstName":"А"}`},
		{"GET, пользователь удалён", fakeIdentity{99, true}, http.MethodGet, ""},
		{"PATCH, пользователь удалён", fakeIdentity{99, true}, http.MethodPatch, `{"firstName":"А"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(newServer(newFakeStore(), tt.identity), tt.method, "/users/me", tt.body)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401, body: %s", rec.Code, rec.Body)
			}
		})
	}
}
