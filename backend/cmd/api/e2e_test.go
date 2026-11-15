package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"

	"builder/backend/internal/platform"
	"builder/backend/internal/platform/pgtest"
	"builder/backend/internal/split"
)

// e2e поднимает ту же сборку, что работает в проде (newRouter), на настоящем
// Postgres. Пул в 4 соединения ловит места, где транзакция просит второе
// соединение при занятом первом.
type e2e struct {
	t    *testing.T
	e    *echo.Echo
	pool *pgxpool.Pool
	hub  *split.Hub
	seq  int
}

func newE2E(t *testing.T) *e2e {
	t.Helper()
	pool := pgtest.NewWithMaxConns(t, 4)
	hub := split.NewHub()
	t.Cleanup(hub.Close)
	cfg := platform.Config{
		JWTSecret:     []byte(strings.Repeat("s", 32)),
		AccessTTL:     time.Hour,
		RefreshTTL:    time.Hour,
		InviteBaseURL: "http://front.test",
	}
	e, err := newRouter(slog.New(slog.NewTextHandler(discard{}, nil)), cfg, pool, hub)
	if err != nil {
		t.Fatal(err)
	}
	return &e2e{t: t, e: e, pool: pool, hub: hub}
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

func (x *e2e) call(method, path, token string, body any) *httptest.ResponseRecorder {
	x.t.Helper()
	var rd *strings.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	} else {
		rd = strings.NewReader("")
	}
	req := httptest.NewRequest(method, apiBaseURL+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	x.e.ServeHTTP(rec, req)
	return rec
}

func (x *e2e) ok(rec *httptest.ResponseRecorder, want int, into any) {
	x.t.Helper()
	if rec.Code != want {
		x.t.Fatalf("status = %d, want %d, body = %s", rec.Code, want, rec.Body)
	}
	if into != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), into); err != nil {
			x.t.Fatalf("тело не JSON: %q", rec.Body)
		}
	}
}

func errCode(rec *httptest.ResponseRecorder) string {
	var b struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &b)
	return b.Code
}

type person struct {
	id    int64
	token string
}

func (x *e2e) register(prefix string) person {
	x.t.Helper()
	x.seq++
	var out struct {
		AccessToken string `json:"accessToken"`
		User        struct {
			ID int64 `json:"id"`
		} `json:"user"`
	}
	x.ok(x.call("POST", "/auth/register", "", map[string]any{
		"login": fmt.Sprintf("%s%d", prefix, x.seq), "password": "Passw0rd!123",
		"firstName": "Имя", "lastName": fmt.Sprintf("Фамилия%d", x.seq),
	}), http.StatusCreated, &out)
	return person{id: out.User.ID, token: out.AccessToken}
}

// openSplit создаёт открытое пространство: teamSize=3, Backend 1..2, Frontend 1..2.
func (x *e2e) openSplit(owner person, policy string) (splitID int64, invite string) {
	x.t.Helper()
	var sp struct {
		ID int64 `json:"id"`
	}
	x.ok(x.call("POST", "/splits", owner.token, map[string]any{
		"title": "Практикум", "teamSize": 3, "remainderPolicy": policy,
		"roles": []map[string]any{
			{"name": "Backend", "perTeamMin": 1, "perTeamMax": 2},
			{"name": "Frontend", "perTeamMin": 1, "perTeamMax": 2},
		},
	}), http.StatusCreated, &sp)
	x.ok(x.call("POST", fmt.Sprintf("/splits/%d/status", sp.ID), owner.token, map[string]string{"status": "Open"}), http.StatusOK, nil)
	var inv struct {
		Token string `json:"token"`
	}
	x.ok(x.call("POST", fmt.Sprintf("/splits/%d/invites", sp.ID), owner.token, map[string]any{}), http.StatusCreated, &inv)
	return sp.ID, inv.Token
}

// join регистрирует студента и вступает в пространство; возвращает ответ join.
func (x *e2e) joinNew(invite string, n int) (person, *httptest.ResponseRecorder) {
	x.t.Helper()
	p := x.register("stud")
	role := "Frontend"
	if n%2 == 1 {
		role = "Backend"
	}
	var f struct {
		ID int64 `json:"id"`
	}
	x.ok(x.call("POST", "/forms", p.token, map[string]any{
		"name": "Анкета", "experience": n % 6, "skills": []string{"go"},
		"rolePreferences": []map[string]any{{"roleName": role, "rank": 1}},
	}), http.StatusCreated, &f)
	return p, x.call("POST", "/members/join", p.token, map[string]any{"inviteToken": invite, "formId": f.ID})
}

func (x *e2e) countRows(query string, args ...any) int {
	x.t.Helper()
	var n int
	if err := x.pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		x.t.Fatal(err)
	}
	return n
}

type runResp struct {
	ID        int64 `json:"id"`
	Seed      int64 `json:"seed"`
	Published bool  `json:"published"`
	Teams     []struct {
		ID      int64 `json:"id"`
		Members []struct {
			MemberID     int64  `json:"memberId"`
			AssignedRole string `json:"assignedRole"`
		} `json:"members"`
	} `json:"teams"`
	Metrics struct {
		RoleQuotasSatisfied bool `json:"roleQuotasSatisfied"`
	} `json:"metrics"`
}

func (r runResp) composition() string {
	var parts []string
	for _, t := range r.Teams {
		var ms []string
		for _, m := range t.Members {
			ms = append(ms, fmt.Sprintf("%d:%s", m.MemberID, m.AssignedRole))
		}
		parts = append(parts, strings.Join(ms, ","))
	}
	return strings.Join(parts, " | ")
}

// Полный жизненный цикл: вступления → preview → смена состава → публикация →
// чтение результата всеми участниками.
func TestE2EDistributionLifecycle(t *testing.T) {
	x := newE2E(t)
	owner := x.register("owner")
	sid, invite := x.openSplit(owner, "spread")
	path := func(suffix string) string { return fmt.Sprintf("/splits/%d%s", sid, suffix) }

	var students []person
	for i := 0; i < 8; i++ {
		p, rec := x.joinNew(invite, i)
		x.ok(rec, http.StatusCreated, nil)
		students = append(students, p)
	}

	var a, b runResp
	x.ok(x.call("POST", path("/distribution/preview"), owner.token, map[string]any{"seed": 42}), 200, &a)
	x.ok(x.call("POST", path("/distribution/preview"), owner.token, map[string]any{"seed": 42}), 200, &b)
	if a.ID == b.ID || a.composition() != b.composition() || a.Published || !a.Metrics.RoleQuotasSatisfied {
		t.Fatalf("preview не детерминирован или опубликован:\n%s\n%s", a.composition(), b.composition())
	}
	if x.countRows(`SELECT count(*) FROM teams`) != 0 {
		t.Fatal("preview создал команды")
	}

	t.Run("состав изменился после preview — roster_changed", func(t *testing.T) {
		_, rec := x.joinNew(invite, 9)
		x.ok(rec, http.StatusCreated, nil)
		rec = x.call("POST", path("/distribution/publish"), owner.token, map[string]any{"runId": a.ID})
		if rec.Code != 409 || errCode(rec) != "roster_changed" {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
		}
		if x.countRows(`SELECT count(*) FROM teams`) != 0 {
			t.Fatal("отклонённая публикация оставила команды")
		}
	})

	var fresh, published runResp
	x.ok(x.call("POST", path("/distribution/preview"), owner.token, map[string]any{"seed": 7}), 200, &fresh)
	x.ok(x.call("POST", path("/distribution/publish"), owner.token, map[string]any{"runId": fresh.ID}), 200, &published)
	if !published.Published || published.ID != fresh.ID || published.composition() != fresh.composition() {
		t.Fatalf("опубликован не тот прогон:\n%s\n%s", published.composition(), fresh.composition())
	}

	t.Run("результат записан в таблицы всех модулей", func(t *testing.T) {
		nTeams := len(published.Teams)
		if nTeams != 3 { // 9 участников, teamSize 3 → 3 команды
			t.Fatalf("команд %d", nTeams)
		}
		if x.countRows(`SELECT count(*) FROM teams WHERE split_id=$1`, sid) != nTeams {
			t.Error("команды не записаны")
		}
		if x.countRows(`SELECT count(*) FROM members WHERE split_id=$1 AND (team_id IS NULL OR assigned_role IS NULL)`, sid) != 0 {
			t.Error("есть участники без команды или роли")
		}
		if x.countRows(`SELECT count(*) FROM splits WHERE id=$1 AND status='Finished'`, sid) != 1 {
			t.Error("пространство не Finished")
		}
		if x.countRows(`SELECT count(*) FROM distribution_runs WHERE split_id=$1 AND published_at IS NOT NULL`, sid) != 1 {
			t.Error("опубликованных прогонов не один")
		}
		var sp struct {
			Status       string `json:"status"`
			MembersCount int    `json:"membersCount"`
			TeamsCount   *int   `json:"teamsCount"`
		}
		x.ok(x.call("GET", path(""), owner.token, nil), 200, &sp)
		if sp.Status != "Finished" || sp.MembersCount != 9 || sp.TeamsCount == nil || *sp.TeamsCount != 3 {
			t.Errorf("пространство = %+v", sp)
		}
	})

	t.Run("повторный publish идемпотентен", func(t *testing.T) {
		var again runResp
		x.ok(x.call("POST", path("/distribution/publish"), owner.token, nil), 200, &again)
		if again.ID != published.ID || again.composition() != published.composition() {
			t.Fatal("повтор вернул другой результат")
		}
		if x.countRows(`SELECT count(*) FROM teams WHERE split_id=$1`, sid) != 3 || x.countRows(`SELECT count(*) FROM distribution_runs WHERE published_at IS NOT NULL`) != 1 {
			t.Error("повтор создал лишние команды или прогоны")
		}
		if rec := x.call("POST", path("/distribution/preview"), owner.token, nil); errCode(rec) != "already_finished" {
			t.Errorf("preview после публикации: %d %s", rec.Code, rec.Body)
		}
	})

	t.Run("студент видит свою команду и участие", func(t *testing.T) {
		var mine struct {
			Name    string `json:"name"`
			Members []struct {
				User struct {
					ID int64 `json:"id"`
				} `json:"user"`
			} `json:"members"`
		}
		x.ok(x.call("GET", path("/teams/me"), students[0].token, nil), 200, &mine)
		found := false
		for _, m := range mine.Members {
			found = found || m.User.ID == students[0].id
		}
		if !found || len(mine.Members) < 3 {
			t.Fatalf("команда = %+v", mine)
		}
		var ms struct {
			TeamID       *int64  `json:"teamId"`
			AssignedRole *string `json:"assignedRole"`
		}
		x.ok(x.call("GET", path("/members/me"), students[0].token, nil), 200, &ms)
		if ms.TeamID == nil || ms.AssignedRole == nil {
			t.Errorf("members/me = %+v", ms)
		}
		if strings.Contains(x.call("GET", path("/teams/me"), students[0].token, nil).Body.String(), "experience") {
			t.Error("анкеты сокомандников утекли")
		}
	})

	t.Run("владелец видит все команды и выгрузку, студент нет", func(t *testing.T) {
		var teams []struct{ Name string }
		x.ok(x.call("GET", path("/teams"), owner.token, nil), 200, &teams)
		if len(teams) != 3 {
			t.Errorf("команд %d", len(teams))
		}
		if rec := x.call("GET", path("/teams"), students[0].token, nil); rec.Code != 403 {
			t.Errorf("студент: %d", rec.Code)
		}
		rec := x.call("GET", path("/teams/export"), owner.token, nil)
		if rec.Code != 200 || !strings.HasPrefix(rec.Body.String(), "\xEF\xBB\xBF") || !strings.Contains(rec.Body.String(), "Команда 1") {
			t.Errorf("выгрузка: %d %q", rec.Code, rec.Body)
		}
	})

	t.Run("после публикации вступить и выйти нельзя", func(t *testing.T) {
		if _, rec := x.joinNew(invite, 3); errCode(rec) != "split_not_open" {
			t.Errorf("join: %d %s", rec.Code, rec.Body)
		}
		if rec := x.call("DELETE", path("/members/me"), students[0].token, nil); errCode(rec) != "split_finished" {
			t.Errorf("leave: %d %s", rec.Code, rec.Body)
		}
	})

}

// Вступления гонятся с публикацией: каждый принятый участник попадает в
// команду, а опоздавшие получают split_not_open. Никто не остаётся вне команд.
func TestE2EJoinRacesPublish(t *testing.T) {
	x := newE2E(t)
	owner := x.register("owner")
	sid, invite := x.openSplit(owner, "spread")
	for i := 0; i < 6; i++ {
		_, rec := x.joinNew(invite, i)
		x.ok(rec, http.StatusCreated, nil)
	}

	// формы и пользователи готовятся заранее, в гонке участвуют только запросы вступления
	type joiner struct {
		p      person
		formID int64
	}
	var joiners []joiner
	for i := 0; i < 10; i++ {
		p := x.register("late")
		var f struct {
			ID int64 `json:"id"`
		}
		x.ok(x.call("POST", "/forms", p.token, map[string]any{"name": "A", "experience": 1}), http.StatusCreated, &f)
		joiners = append(joiners, joiner{p, f.ID})
	}

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		accepted int
		start    = make(chan struct{})
	)
	for _, j := range joiners {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			rec := x.call("POST", "/members/join", j.p.token, map[string]any{"inviteToken": invite, "formId": j.formID})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case rec.Code == http.StatusCreated:
				accepted++
			case errCode(rec) == "split_not_open":
			default:
				t.Errorf("join: status = %d, body = %s", rec.Code, rec.Body)
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		rec := x.call("POST", fmt.Sprintf("/splits/%d/distribution/publish", sid), owner.token, nil)
		if rec.Code != 200 {
			t.Errorf("publish: status = %d, body = %s", rec.Code, rec.Body)
		}
	}()
	close(start)
	wg.Wait()

	total := x.countRows(`SELECT count(*) FROM members WHERE split_id=$1`, sid)
	if total != 6+accepted {
		t.Fatalf("в таблице %d участников, принято вступлений %d из 10", total, accepted)
	}
	if n := x.countRows(`SELECT count(*) FROM members WHERE split_id=$1 AND team_id IS NULL`, sid); n != 0 {
		t.Fatalf("%d участников вне команд: вступление прошло после публикации", n)
	}
	// инвайт расходуется только на принятых: отказавшие вернули использование
	if uses := x.countRows(`SELECT uses_count FROM invites WHERE split_id=$1`, sid); uses != 6+accepted {
		t.Fatalf("uses_count = %d, принято %d", uses, 6+accepted)
	}
}
