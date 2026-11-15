package tests

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"builder/backend/internal/member"
	"builder/backend/internal/platform/pgtest"
)

// Чужие таблицы нужны тестам только как предпосылка внешних ключей, поэтому
// создаются прямым SQL.
type pgFixture struct {
	pool    *pgxpool.Pool
	ownerID int64
	splitID int64
}

func newPgFixture(t *testing.T, maxMembers *int) pgFixture {
	t.Helper()
	return newPgFixtureOn(t, pgtest.New(t), maxMembers)
}

func newPgFixtureOn(t *testing.T, pool *pgxpool.Pool, maxMembers *int) pgFixture {
	t.Helper()
	f := pgFixture{pool: pool, ownerID: newUser(t, pool, "owner")}
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO splits (owner_id, title, team_size, status, max_members) VALUES ($1, 'Практикум', 5, 'Open', $2) RETURNING id`,
		f.ownerID, maxMembers).Scan(&f.splitID); err != nil {
		t.Fatal(err)
	}
	return f
}

func newUser(t *testing.T, pool *pgxpool.Pool, login string) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO users (login, password_hash, first_name, last_name) VALUES ($1, 'h', 'Имя', 'Фамилия') RETURNING id`,
		login).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func newForm(t *testing.T, pool *pgxpool.Pool, userID int64) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO forms (user_id, name, experience) VALUES ($1, 'Моя', 4) RETURNING id`, userID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestRepositoryOnPostgres(t *testing.T) {
	ctx := context.Background()
	f := newPgFixture(t, nil)
	repo := member.NewRepository(f.pool)
	uid := newUser(t, f.pool, "student")
	formID := newForm(t, f.pool, uid)
	comment := "хочу в бэкенд"

	snap := member.FormSnapshot{
		Experience: 4, Skills: []string{"go", "sql"}, Comment: &comment,
		RolePreferences: []member.RolePreference{{RoleName: "Backend", Rank: 1}},
	}
	m, err := repo.Insert(ctx, member.NewMember{SplitID: f.splitID, UserID: uid, FormID: formID, Snapshot: snap})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("снимок читается обратно без потерь", func(t *testing.T) {
		got, err := repo.Get(ctx, m.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.TeamID != nil || got.AssignedRole != nil {
			t.Errorf("team_id/assigned_role должны быть null: %+v", got)
		}
		s := got.Snapshot
		if s.Experience != 4 || len(s.Skills) != 2 || s.Comment == nil || *s.Comment != comment ||
			len(s.RolePreferences) != 1 || s.RolePreferences[0].RoleName != "Backend" {
			t.Fatalf("snapshot = %+v", s)
		}
	})

	t.Run("UNIQUE(split_id, user_id)", func(t *testing.T) {
		_, err := repo.Insert(ctx, member.NewMember{SplitID: f.splitID, UserID: uid, FormID: formID})
		if !errors.Is(err, member.ErrAlreadyMember) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("запросы для split", func(t *testing.T) {
		if ok, _ := repo.IsMember(ctx, f.splitID, uid); !ok {
			t.Error("IsMember = false")
		}
		if ok, _ := repo.IsMember(ctx, f.splitID, f.ownerID); ok {
			t.Error("владелец не участник")
		}
		ids, _ := repo.SplitIDsOfUser(ctx, uid)
		if len(ids) != 1 || ids[0] != f.splitID {
			t.Errorf("ids = %v", ids)
		}
		c, _ := repo.Counts(ctx, []int64{f.splitID, 99999})
		if c[f.splitID] != 1 || len(c) != 1 {
			t.Errorf("counts = %v", c)
		}
		if _, err := repo.Counts(ctx, nil); err != nil {
			t.Errorf("nil-список: %v", err)
		}
	})

	t.Run("удаление анкеты не ломает участие", func(t *testing.T) {
		if _, err := f.pool.Exec(ctx, `DELETE FROM forms WHERE id = $1`, formID); err != nil {
			t.Fatal(err)
		}
		got, err := repo.Get(ctx, m.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.FormID != nil || got.Snapshot.Experience != 4 {
			t.Fatalf("form_id=%v snapshot=%+v", got.FormID, got.Snapshot)
		}
	})

	t.Run("удаление и отсутствие", func(t *testing.T) {
		if err := repo.Delete(ctx, m.ID); err != nil {
			t.Fatal(err)
		}
		if err := repo.Delete(ctx, m.ID); !errors.Is(err, member.ErrNotFound) {
			t.Fatalf("повторное удаление: %v", err)
		}
		if _, err := repo.GetByUser(ctx, f.splitID, uid); !errors.Is(err, member.ErrNotFound) {
			t.Fatalf("GetByUser: %v", err)
		}
	})

	t.Run("удаление пространства каскадом убирает участников", func(t *testing.T) {
		u2 := newUser(t, f.pool, "second")
		if _, err := repo.Insert(ctx, member.NewMember{SplitID: f.splitID, UserID: u2, FormID: 0}); err == nil {
			// form_id=0 нарушает внешний ключ — проверяем, что это не молчаливый успех
			t.Fatal("вставка с несуществующей анкетой прошла")
		}
		fid := newForm(t, f.pool, u2)
		if _, err := repo.Insert(ctx, member.NewMember{SplitID: f.splitID, UserID: u2, FormID: fid}); err != nil {
			t.Fatal(err)
		}
		if _, err := f.pool.Exec(ctx, `DELETE FROM splits WHERE id = $1`, f.splitID); err != nil {
			t.Fatal(err)
		}
		if n, _ := repo.Count(ctx, f.splitID); n != 0 {
			t.Fatalf("осталось %d участников", n)
		}
	})
}

// Сквозной сценарий с настоящим Postgres и маленьким пулом: параллельные
// вступления не превышают maxMembers, не занимают лишних использований
// инвайта и не вешают пул (сервис держит соединение транзакции и просит
// второе для чтения пространства).
func TestJoinRaceOnPostgres(t *testing.T) {
	const limit, attempts = 3, 24
	ctx := context.Background()
	pool := pgtest.NewWithMaxConns(t, 4)
	f := newPgFixtureOn(t, pool, ptr(limit))

	var inviteID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO invites (split_id, token) VALUES ($1, 'tok') RETURNING id`, f.splitID).Scan(&inviteID); err != nil {
		t.Fatal(err)
	}

	repo := member.NewRepository(pool)
	inv := &pgInvites{pool: pool}
	svc := member.NewService(repo, member.NewPgTxRunner(pool), inv,
		fakeForms{snap: member.FormSnapshot{Experience: 1}},
		&pgSplits{pool: pool}, &fakeUsers{}, &fakeEvents{})

	// form_id — внешний ключ, поэтому у каждого вступающего своя настоящая анкета
	type applicant struct{ userID, formID int64 }
	applicants := make([]applicant, attempts)
	for i := range applicants {
		uid := newUser(t, pool, "s"+itoa(int64(i)))
		applicants[i] = applicant{userID: uid, formID: newForm(t, pool, uid)}
	}

	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		ok, ff int
	)
	for _, a := range applicants {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.Join(ctx, a.userID, "tok", a.formID)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case errors.Is(err, member.ErrSplitFull):
				ff++
			default:
				t.Errorf("неожиданная ошибка: %v", err)
			}
		}()
	}
	wg.Wait()

	if ok != limit || ff != attempts-limit {
		t.Fatalf("принято %d, отказано %d", ok, ff)
	}
	if n, _ := repo.Count(ctx, f.splitID); n != limit {
		t.Fatalf("в таблице %d участников", n)
	}
	var uses int
	if err := pool.QueryRow(ctx, `SELECT uses_count FROM invites WHERE id = $1`, inviteID).Scan(&uses); err != nil {
		t.Fatal(err)
	}
	if uses != limit {
		t.Fatalf("uses_count = %d, ждали %d: отказавшие не вернули использование", uses, limit)
	}
}

// pgInvites и pgSplits читают чужие таблицы напрямую — только в тесте, вместо
// настоящих модулей, чтобы не тянуть их сборку в тесты member.
type pgInvites struct{ pool *pgxpool.Pool }

func (p *pgInvites) Redeem(ctx context.Context, token string) (member.Redemption, error) {
	var r member.Redemption
	err := p.pool.QueryRow(ctx,
		`UPDATE invites SET uses_count = uses_count + 1 WHERE token = $1 RETURNING id, split_id`, token).
		Scan(&r.InviteID, &r.SplitID)
	return r, err
}

func (p *pgInvites) Release(ctx context.Context, id int64) error {
	_, err := p.pool.Exec(ctx, `UPDATE invites SET uses_count = GREATEST(uses_count - 1, 0) WHERE id = $1`, id)
	return err
}

type pgSplits struct{ pool *pgxpool.Pool }

func (p *pgSplits) SplitInfo(ctx context.Context, id int64) (member.SplitInfo, error) {
	var info member.SplitInfo
	var max *int32
	err := p.pool.QueryRow(ctx, `SELECT owner_id, status, max_members FROM splits WHERE id = $1`, id).
		Scan(&info.OwnerID, &info.Status, &max)
	if max != nil {
		v := int(*max)
		info.MaxMembers = &v
	}
	return info, err
}
