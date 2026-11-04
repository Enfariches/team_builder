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
	"builder/backend/internal/invite"
	"builder/backend/internal/platform"
)

type fakeIdentity struct{ id int64 }

func (f fakeIdentity) UserID(context.Context) (int64, bool) { return f.id, true }

type inviteAPI = invite.Handler

// Вложенная структура сдвигает методы nil-интерфейса на уровень глубже,
// поэтому методы invite.Handler побеждают без неоднозначности.
type unimplemented struct{ gen.StrictServerInterface }

type srv struct {
	unimplemented
	*inviteAPI
}

func newServer(svc *invite.Service, user int64) *echo.Echo {
	e := echo.New()
	e.HTTPErrorHandler = platform.NewErrorHandler(slog.Default(), invite.HTTPErrors...)
	h := invite.NewHandler(svc, fakeIdentity{user}, "http://front.test/")
	gen.RegisterHandlers(e, gen.NewStrictHandler(srv{inviteAPI: h}, nil))
	return e
}

func do(e *echo.Echo, method, path, body string) *httptest.ResponseRecorder {
	var r *strings.Reader
	if body != "" {
		r = strings.NewReader(body)
	} else {
		r = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, r)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestCreateInviteWithoutBody(t *testing.T) {
	svc, _, _ := newService()
	rec := do(newServer(svc, owner), http.MethodPost, "/splits/100/invites", "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var got gen.Invite
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Url != "http://front.test/join/"+got.Token || got.Token == "" || got.UsesCount != 0 {
		t.Fatalf("invite = %+v", got)
	}
}

func TestCreateInviteErrors(t *testing.T) {
	tests := []struct {
		name   string
		user   int64
		path   string
		body   string
		status int
		code   string
	}{
		{"прошлое expiresAt", owner, "/splits/100/invites", `{"expiresAt":"2020-01-01T00:00:00Z"}`, 422, "validation_failed"},
		{"maxUses 0", owner, "/splits/100/invites", `{"maxUses":0}`, 422, "validation_failed"},
		{"участник", member, "/splits/100/invites", `{}`, 403, "forbidden"},
		{"посторонний", stranger, "/splits/100/invites", `{}`, 404, "not_found"},
		{"битый JSON", owner, "/splits/100/invites", `{`, 400, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, _ := newService()
			rec := do(newServer(svc, tt.user), http.MethodPost, tt.path, tt.body)
			if rec.Code != tt.status || (tt.code != "" && !strings.Contains(rec.Body.String(), `"`+tt.code+`"`)) {
				t.Fatalf("got %d %s, want %d %s", rec.Code, rec.Body, tt.status, tt.code)
			}
		})
	}
}

func TestListAndRevoke(t *testing.T) {
	svc, _, _ := newService()
	e := newServer(svc, owner)
	do(e, http.MethodPost, "/splits/100/invites", "")

	rec := do(e, http.MethodGet, "/splits/100/invites", "")
	var list []gen.Invite
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if rec.Code != 200 || len(list) != 1 {
		t.Fatalf("список: %d %s", rec.Code, rec.Body)
	}

	for i := 0; i < 2; i++ {
		if rec := do(e, http.MethodDelete, "/splits/100/invites/1", ""); rec.Code != http.StatusNoContent {
			t.Fatalf("отзыв #%d: %d %s", i+1, rec.Code, rec.Body)
		}
	}
	if rec := do(e, http.MethodDelete, "/splits/100/invites/99", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("чужой инвайт: %d", rec.Code)
	}
	if rec := do(newServer(svc, member), http.MethodGet, "/splits/100/invites", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("участник: %d", rec.Code)
	}
}

func TestPreviewEndpoint(t *testing.T) {
	svc, _, _ := newService()
	e := newServer(svc, owner)
	var inv gen.Invite
	_ = json.Unmarshal(do(e, http.MethodPost, "/splits/100/invites", "").Body.Bytes(), &inv)

	rec := do(e, http.MethodGet, "/invites/"+inv.Token, "")
	var p gen.InvitePreview
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	if rec.Code != 200 || p.SplitTitle != "Практикум" || len(p.RoleNames) != 1 || p.Status != gen.SplitStatus("Open") {
		t.Fatalf("preview: %d %s", rec.Code, rec.Body)
	}

	do(e, http.MethodDelete, "/splits/100/invites/1", "")
	rec = do(e, http.MethodGet, "/invites/"+inv.Token, "")
	if rec.Code != 404 || !strings.Contains(rec.Body.String(), `"invite_not_found"`) {
		t.Fatalf("после отзыва: %d %s", rec.Code, rec.Body)
	}
}
