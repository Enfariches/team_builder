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
	"builder/backend/internal/form"
	"builder/backend/internal/platform"
)

type fakeIdentity struct {
	id int64
	ok bool
}

func (f fakeIdentity) UserID(context.Context) (int64, bool) { return f.id, f.ok }

type formAPI = form.Handler

// Вложенная структура сдвигает методы nil-интерфейса на уровень глубже,
// поэтому методы form.Handler побеждают без неоднозначности.
type unimplemented struct{ gen.StrictServerInterface }

type srv struct {
	unimplemented
	*formAPI
}

func newServer(st *fakeStore, id fakeIdentity) *echo.Echo {
	e := echo.New()
	e.HTTPErrorHandler = platform.NewErrorHandler(slog.Default(), form.HTTPErrors...)
	h := form.NewHandler(form.NewService(st), id)
	gen.RegisterHandlers(e, gen.NewStrictHandler(srv{formAPI: h}, nil))
	return e
}

func do(e *echo.Echo, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestCreateForm(t *testing.T) {
	e := newServer(newFakeStore(), fakeIdentity{1, true})

	t.Run("201 с умолчаниями", func(t *testing.T) {
		rec := do(e, http.MethodPost, "/forms", `{"name":" Анкета "}`)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body)
		}
		var got map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &got)
		if got["name"] != "Анкета" || got["experience"] != float64(0) {
			t.Fatalf("body = %v", got)
		}
		if s, ok := got["skills"].([]any); !ok || len(s) != 0 {
			t.Fatalf("skills должен быть [], got %v", got["skills"])
		}
		if p, ok := got["rolePreferences"].([]any); !ok || len(p) != 0 {
			t.Fatalf("rolePreferences должен быть [], got %v", got["rolePreferences"])
		}
	})

	t.Run("422 с details", func(t *testing.T) {
		rec := do(e, http.MethodPost, "/forms", `{"name":"a","experience":9,"rolePreferences":[{"roleName":"A","rank":1},{"roleName":"B","rank":1}]}`)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body)
		}
		if !strings.Contains(rec.Body.String(), "rolePreferences[1].rank") {
			t.Fatalf("в details нет поля: %s", rec.Body)
		}
	})

	t.Run("400 на битый JSON", func(t *testing.T) {
		if rec := do(e, http.MethodPost, "/forms", `{`); rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", rec.Code)
		}
	})
}

func TestPatchForm(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantCalls  int
		check      func(t *testing.T, u form.Update)
	}{
		{"comment не передан", `{"name":"Х"}`, 200, 1, func(t *testing.T, u form.Update) {
			if u.Comment.Set {
				t.Fatalf("comment не передан, но Set: %+v", u.Comment)
			}
		}},
		{"comment null очищает", `{"comment":null}`, 200, 1, func(t *testing.T, u form.Update) {
			if !u.Comment.Set || u.Comment.Value != nil {
				t.Fatalf("Comment = %+v, want Set && nil", u.Comment)
			}
		}},
		{"comment строка", `{"comment":" привет "}`, 200, 1, func(t *testing.T, u form.Update) {
			if !u.Comment.Set || u.Comment.Value == nil || *u.Comment.Value != "привет" {
				t.Fatalf("Comment = %+v", u.Comment)
			}
		}},
		{"пустой навыков заменяет", `{"skills":[]}`, 200, 1, func(t *testing.T, u form.Update) {
			if u.Skills == nil || len(*u.Skills) != 0 {
				t.Fatalf("Skills = %v, want пустой непустой указатель", u.Skills)
			}
		}},
		{"пустое тело", `{}`, 200, 0, nil},
		{"пустое имя", `{"name":""}`, 422, 0, nil},
		{"name null", `{"name":null}`, 200, 0, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newFakeStore()
			rec := do(newServer(st, fakeIdentity{1, true}), http.MethodPatch, "/forms/1", tt.body)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.wantStatus, rec.Body)
			}
			if st.updateCalls != tt.wantCalls {
				t.Fatalf("updateCalls = %d, want %d", st.updateCalls, tt.wantCalls)
			}
			if tt.check != nil {
				tt.check(t, st.lastUpdate)
			}
		})
	}
}

func TestForeignFormReturns404(t *testing.T) {
	e := newServer(newFakeStore(), fakeIdentity{1, true})
	for _, c := range []struct{ method, body string }{
		{http.MethodGet, ""},
		{http.MethodPatch, `{"name":"x"}`},
		{http.MethodDelete, ""},
	} {
		rec := do(e, c.method, "/forms/2", c.body)
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), `"not_found"`) {
			t.Errorf("%s чужой анкеты: %d %s", c.method, rec.Code, rec.Body)
		}
	}
}

func TestListAndDelete(t *testing.T) {
	e := newServer(newFakeStore(), fakeIdentity{1, true})

	rec := do(e, http.MethodGet, "/forms", "")
	var list []gen.Form
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if rec.Code != 200 || len(list) != 1 || list[0].Id != 1 {
		t.Fatalf("список: %d %s", rec.Code, rec.Body)
	}

	if rec := do(e, http.MethodDelete, "/forms/1", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d", rec.Code)
	}
	if rec := do(e, http.MethodGet, "/forms/1", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("после удаления status = %d", rec.Code)
	}
}
