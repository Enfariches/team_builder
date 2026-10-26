package tests

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v5"

	"builder/backend/internal/api/gen"
	"builder/backend/internal/platform"
	"builder/backend/internal/split"
)

type uidKey struct{}

// headerIdentity берёт id пользователя из заголовка X-Test-User, который
// middleware кладёт в контекст: так тест выбирает «кто делает запрос».
type headerIdentity struct{}

func (headerIdentity) UserID(ctx context.Context) (int64, bool) {
	id, ok := ctx.Value(uidKey{}).(int64)
	return id, ok
}

type unimplemented struct{ gen.StrictServerInterface }

type splitAPI = split.Handler

type srv struct {
	unimplemented
	*splitAPI
}

func newServer(svc *split.Service, hub *split.Hub, ping time.Duration) *echo.Echo {
	e := echo.New()
	e.HTTPErrorHandler = platform.NewErrorHandler(slog.Default(), split.HTTPErrors...)
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if v := c.Request().Header.Get("X-Test-User"); v != "" {
				id, _ := strconv.ParseInt(v, 10, 64)
				c.SetRequest(c.Request().WithContext(context.WithValue(c.Request().Context(), uidKey{}, id)))
			}
			return next(c)
		}
	})
	h := split.NewHandler(svc, hub, headerIdentity{}, ping)
	gen.RegisterHandlers(e, gen.NewStrictHandler(srv{splitAPI: h}, nil))
	return e
}

func call(e http.Handler, uid int64, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if uid != 0 {
		req.Header.Set("X-Test-User", strconv.FormatInt(uid, 10))
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

const createBody = `{"title":"Практикум","teamSize":5,"roles":[
  {"name":"Backend","perTeamMin":1,"perTeamMax":2},
  {"name":"Frontend","perTeamMin":1,"perTeamMax":2},
  {"name":"Designer","perTeamMin":1,"perTeamMax":1},
  {"name":"Analyst","perTeamMin":0,"perTeamMax":1}]}`

type errBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details []struct {
		Field   string `json:"field"`
		Message string `json:"message"`
	} `json:"details"`
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body, err)
	}
	return v
}

func TestCreateSplitHTTP(t *testing.T) {
	st, mem, hub := newFakeStore(), newFakeMembers(), split.NewHub()
	e := newServer(split.NewService(st, st, mem, mem, hub), hub, time.Hour)

	t.Run("201 с примером из спеки", func(t *testing.T) {
		rec := call(e, owner, http.MethodPost, "/splits", createBody)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, body: %s", rec.Code, rec.Body)
		}
		sp := decode[gen.Split](t, rec)
		if *sp.Status != gen.Draft || *sp.MyRole != gen.SplitMyRoleOwner || *sp.RemainderPolicy != gen.Spread || len(*sp.Roles) != 4 {
			t.Fatalf("%+v", sp)
		}
	})

	t.Run("422 invalid_team_rules с details", func(t *testing.T) {
		body := `{"title":"X","teamSize":5,"roles":[{"name":"A","perTeamMin":3,"perTeamMax":3},{"name":"B","perTeamMin":3,"perTeamMax":3}]}`
		rec := call(e, owner, http.MethodPost, "/splits", body)
		eb := decode[errBody](t, rec)
		if rec.Code != 422 || eb.Code != "invalid_team_rules" || len(eb.Details) != 1 ||
			eb.Details[0].Message != "сумма минимальных квот (6) больше размера команды (5)" {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
		}
	})

	t.Run("422 validation_failed без нарушения правил", func(t *testing.T) {
		rec := call(e, owner, http.MethodPost, "/splits", `{"title":" ","teamSize":3,"roles":[{"name":"A","perTeamMin":1,"perTeamMax":3}]}`)
		if eb := decode[errBody](t, rec); rec.Code != 422 || eb.Code != "validation_failed" {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
		}
	})

	t.Run("400 на битый JSON", func(t *testing.T) {
		if rec := call(e, owner, http.MethodPost, "/splits", `{`); rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", rec.Code)
		}
	})
}

func TestSplitHTTPFlow(t *testing.T) {
	svc, _, _, hub, id := newEnv(t)
	e := newServer(svc, hub, time.Hour)
	path := "/splits/" + strconv.FormatInt(id, 10)

	t.Run("GET: участник 200, посторонний 404", func(t *testing.T) {
		if rec := call(e, member, http.MethodGet, path, ""); rec.Code != 200 || *decode[gen.Split](t, rec).MyRole != gen.SplitMyRoleMember {
			t.Fatalf("участник: %d %s", rec.Code, rec.Body)
		}
		if rec := call(e, stranger, http.MethodGet, path, ""); rec.Code != 404 {
			t.Fatalf("посторонний: %d", rec.Code)
		}
	})

	t.Run("PATCH: участник 403", func(t *testing.T) {
		if rec := call(e, member, http.MethodPatch, path, `{"title":"Х"}`); rec.Code != 403 {
			t.Fatalf("status = %d", rec.Code)
		}
	})

	t.Run("PATCH: null снимает maxMembers и autoFinishAt, отсутствие не трогает", func(t *testing.T) {
		rec := call(e, owner, http.MethodPatch, path, `{"maxMembers":10,"autoFinishAt":"2030-01-01T10:00:00Z","description":"текст"}`)
		sp := decode[gen.Split](t, rec)
		if rec.Code != 200 || sp.MaxMembers == nil || *sp.MaxMembers != 10 || sp.AutoFinishAt == nil || sp.Description == nil {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
		rec = call(e, owner, http.MethodPatch, path, `{"maxMembers":null,"autoFinishAt":null}`)
		sp = decode[gen.Split](t, rec)
		if sp.MaxMembers != nil || sp.AutoFinishAt != nil || sp.Description == nil {
			t.Fatalf("null не применился или затронул description: %s", rec.Body)
		}
	})

	t.Run("статус: Draft -> Open 200, Open -> Finished 409 с сообщением", func(t *testing.T) {
		if rec := call(e, owner, http.MethodPost, path+"/status", `{"status":"Open"}`); rec.Code != 200 {
			t.Fatalf("status = %d %s", rec.Code, rec.Body)
		}
		rec := call(e, owner, http.MethodPost, path+"/status", `{"status":"Finished"}`)
		eb := decode[errBody](t, rec)
		if rec.Code != 409 || eb.Code != "invalid_status_transition" || eb.Message != "Переход из Open в Finished недопустим" {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
	})

	t.Run("PATCH правил в Open — 409 invalid_status_for_update", func(t *testing.T) {
		rec := call(e, owner, http.MethodPatch, path, `{"teamSize":4}`)
		if eb := decode[errBody](t, rec); rec.Code != 409 || eb.Code != "invalid_status_for_update" {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
	})

	t.Run("список с фильтром по неизвестному статусу — 422", func(t *testing.T) {
		if rec := call(e, owner, http.MethodGet, "/splits?status=Nope", ""); rec.Code != 422 && rec.Code != 400 {
			t.Fatalf("status = %d %s", rec.Code, rec.Body)
		}
	})

	t.Run("список и DELETE", func(t *testing.T) {
		rec := call(e, owner, http.MethodGet, "/splits?role=owner&status=Open", "")
		if list := decode[[]gen.Split](t, rec); rec.Code != 200 || len(list) != 1 {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
		if rec := call(e, member, http.MethodDelete, path, ""); rec.Code != 403 {
			t.Fatalf("участник удаляет: %d", rec.Code)
		}
		if rec := call(e, owner, http.MethodDelete, path, ""); rec.Code != 204 {
			t.Fatalf("владелец удаляет: %d", rec.Code)
		}
		if rec := call(e, owner, http.MethodGet, path, ""); rec.Code != 404 {
			t.Fatalf("после удаления: %d", rec.Code)
		}
	})
}

func TestStreamSplit(t *testing.T) {
	svc, _, _, hub, id := newEnv(t)
	e := newServer(svc, hub, 30*time.Millisecond)
	ts := httptest.NewServer(e)
	defer ts.Close()
	base := ts.URL + "/splits/" + strconv.FormatInt(id, 10)

	do := func(ctx context.Context, uid int64, method, url, body string) *http.Response {
		req, _ := http.NewRequestWithContext(ctx, method, url, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Test-User", strconv.FormatInt(uid, 10))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	t.Run("посторонний не получает поток", func(t *testing.T) {
		resp := do(context.Background(), stranger, http.MethodGet, base+"/stream", "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d", resp.StatusCode)
		}
	})

	t.Run("участник получает status_changed и ping, поток закрывается с контекстом", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		resp := do(ctx, member, http.MethodGet, base+"/stream", "")
		defer resp.Body.Close()
		if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
			t.Fatalf("status=%d ct=%q", resp.StatusCode, resp.Header.Get("Content-Type"))
		}

		lines := make(chan string, 64)
		go func() {
			sc := bufio.NewScanner(resp.Body)
			for sc.Scan() {
				lines <- sc.Text()
			}
			close(lines)
		}()
		waitFor := func(want string) {
			t.Helper()
			deadline := time.After(3 * time.Second)
			for {
				select {
				case l, ok := <-lines:
					if !ok {
						t.Fatalf("поток закрылся, ожидали %q", want)
					}
					if l == want {
						return
					}
				case <-deadline:
					t.Fatalf("не дождались %q", want)
				}
			}
		}

		waitFor(": connected")
		r := do(context.Background(), owner, http.MethodPost, base+"/status", `{"status":"Open"}`)
		r.Body.Close()
		waitFor("event: status_changed")
		waitFor(`data: {"status":"Open"}`)
		waitFor("event: ping")

		cancel()
		deadline := time.After(3 * time.Second)
		for {
			select {
			case _, ok := <-lines:
				if !ok {
					return
				}
			case <-deadline:
				t.Fatal("поток не закрылся после отмены контекста")
			}
		}
	})
}
