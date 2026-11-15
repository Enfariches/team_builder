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
	"builder/backend/internal/member"
	"builder/backend/internal/platform"
)

type fakeIdentity struct{ id int64 }

func (f fakeIdentity) UserID(context.Context) (int64, bool) { return f.id, true }

type memberAPI = member.Handler

// Вложенная структура сдвигает методы nil-интерфейса на уровень глубже,
// поэтому методы member.Handler побеждают без неоднозначности.
type unimplemented struct{ gen.StrictServerInterface }

type srv struct {
	unimplemented
	*memberAPI
}

func newServer(e *env, user int64) *echo.Echo {
	srvr := echo.New()
	srvr.HTTPErrorHandler = platform.NewErrorHandler(slog.Default(), member.HTTPErrors...)
	h := member.NewHandler(e.svc, fakeIdentity{user})
	gen.RegisterHandlers(srvr, gen.NewStrictHandler(srv{memberAPI: h}, nil))
	return srvr
}

func do(e *echo.Echo, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func errCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var b struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatalf("тело ответа не JSON: %q", rec.Body.String())
	}
	return b.Code
}

func TestJoinEndpoint(t *testing.T) {
	const body = `{"inviteToken":"tok","formId":11}`

	t.Run("201 и тело участника без анкеты", func(t *testing.T) {
		e := newEnv(openSplit(nil))
		rec := do(newServer(e, student), http.MethodPost, "/members/join", body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
		}
		var got map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &got)
		if got["role"] != "Member" || got["formId"] != float64(11) || got["userId"] != float64(student) {
			t.Fatalf("body = %v", got)
		}
		if _, leaked := got["formSnapshot"]; leaked {
			t.Fatal("снимок анкеты попал в ответ участнику")
		}
	})

	tests := []struct {
		name     string
		info     member.SplitInfo
		prepare  func(e *env)
		wantCode int
		wantBody string
	}{
		{"нет мест", openSplit(ptr(0)), nil, http.StatusConflict, "split_full"},
		{"набор закрыт", member.SplitInfo{OwnerID: owner, Status: "Closed"}, nil, http.StatusConflict, "split_not_open"},
		{"плохой инвайт", openSplit(nil), func(e *env) { e.invites.redeemErr = member.ErrInviteInvalid }, http.StatusNotFound, "invite_not_found"},
		{"чужая анкета", openSplit(nil), func(e *env) { e.forms.err = member.ErrFormNotFound }, http.StatusNotFound, "form_not_found"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(tc.info)
			if tc.prepare != nil {
				tc.prepare(e)
			}
			rec := do(newServer(e, student), http.MethodPost, "/members/join", body)
			if rec.Code != tc.wantCode || errCode(t, rec) != tc.wantBody {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
			}
		})
	}

	t.Run("повтор — 409 already_member", func(t *testing.T) {
		e := newEnv(openSplit(nil))
		s := newServer(e, student)
		do(s, http.MethodPost, "/members/join", body)
		rec := do(s, http.MethodPost, "/members/join", body)
		if rec.Code != http.StatusConflict || errCode(t, rec) != "already_member" {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
		}
	})
}

// Приватность: анкеты видит только владелец.
func TestListMembersPrivacy(t *testing.T) {
	e := newEnv(openSplit(nil))
	do(newServer(e, student), http.MethodPost, "/members/join", `{"inviteToken":"tok","formId":11}`)

	rec := do(newServer(e, student), http.MethodGet, "/splits/100/members", "")
	if rec.Code != http.StatusForbidden || strings.Contains(rec.Body.String(), "experience") {
		t.Fatalf("участник: status = %d, body = %s", rec.Code, rec.Body)
	}
	rec = do(newServer(e, other), http.MethodGet, "/splits/100/members", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("посторонний: status = %d", rec.Code)
	}

	rec = do(newServer(e, owner), http.MethodGet, "/splits/100/members", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("владелец: status = %d, body = %s", rec.Code, rec.Body)
	}
	var list []struct {
		UserID       int64 `json:"userId"`
		FormSnapshot struct {
			Experience int      `json:"experience"`
			Skills     []string `json:"skills"`
		} `json:"formSnapshot"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list) != 1 {
		t.Fatalf("list = %s err = %v", rec.Body, err)
	}
	if list[0].UserID != student || list[0].FormSnapshot.Experience != 3 || len(list[0].FormSnapshot.Skills) != 1 {
		t.Fatalf("list[0] = %+v", list[0])
	}
}

func TestMembershipLifecycleEndpoints(t *testing.T) {
	e := newEnv(openSplit(nil))
	s := newServer(e, student)

	if rec := do(s, http.MethodGet, "/splits/100/members/me", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("до вступления: %d", rec.Code)
	}
	do(s, http.MethodPost, "/members/join", `{"inviteToken":"tok","formId":11}`)
	if rec := do(s, http.MethodGet, "/splits/100/members/me", ""); rec.Code != http.StatusOK {
		t.Fatalf("после вступления: %d", rec.Code)
	}
	if rec := do(s, http.MethodDelete, "/splits/100/members/me", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("выход: %d", rec.Code)
	}
	if rec := do(s, http.MethodGet, "/splits/100/members/me", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("после выхода: %d", rec.Code)
	}

	t.Run("после публикации выйти нельзя — 409", func(t *testing.T) {
		e := newEnv(member.SplitInfo{OwnerID: owner, Status: member.StatusFinished})
		_, _ = e.store.Insert(context.Background(), member.NewMember{SplitID: splitID, UserID: student, FormID: 1})
		rec := do(newServer(e, student), http.MethodDelete, "/splits/100/members/me", "")
		if rec.Code != http.StatusConflict || errCode(t, rec) != "split_finished" {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
		}
	})

	t.Run("исключение: владелец 204, участник 403", func(t *testing.T) {
		e := newEnv(openSplit(nil))
		v, _ := e.store.Insert(context.Background(), member.NewMember{SplitID: splitID, UserID: student, FormID: 1})
		_, _ = e.store.Insert(context.Background(), member.NewMember{SplitID: splitID, UserID: other, FormID: 1})
		if rec := do(newServer(e, other), http.MethodDelete, "/members/"+itoa(v.ID), ""); rec.Code != http.StatusForbidden {
			t.Fatalf("участник: %d", rec.Code)
		}
		if rec := do(newServer(e, owner), http.MethodDelete, "/members/"+itoa(v.ID), ""); rec.Code != http.StatusNoContent {
			t.Fatalf("владелец: %d", rec.Code)
		}
	})
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }
