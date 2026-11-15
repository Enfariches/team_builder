package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/jackc/pgx/v5/pgxpool"

	"builder/backend/internal/api/gen"
	"builder/backend/internal/auth"
	"builder/backend/internal/form"
	"builder/backend/internal/invite"
	"builder/backend/internal/member"
	"builder/backend/internal/split"
	"builder/backend/internal/user"
)

// notImplemented закрывает операции, чьи модули ещё не написаны. Вызов такой
// операции упадёт в nil-интерфейсе, Recover вернёт 500.
type notImplemented struct{ gen.StrictServerInterface }

// Алиасы нужны, чтобы у встроенных обработчиков были разные имена полей:
// оба типа называются Handler.
type (
	authAPI   = auth.Handler
	userAPI   = user.Handler
	splitAPI  = split.Handler
	formAPI   = form.Handler
	inviteAPI = invite.Handler
	memberAPI = member.Handler
)

type apiServer struct {
	notImplemented
	*authAPI
	*userAPI
	*splitAPI
	*formAPI
	*inviteAPI
	*memberAPI
	pool *pgxpool.Pool
}

func (s *apiServer) GetHealth(ctx context.Context, _ gen.GetHealthRequestObject) (gen.GetHealthResponseObject, error) {
	status, db := gen.Ok, gen.Up
	if err := s.pool.Ping(ctx); err != nil {
		status, db = gen.Degraded, gen.Down
	}
	h := gen.Health{Status: &status, Database: &db}
	if db == gen.Down {
		return gen.GetHealth503JSONResponse{ServiceUnavailableJSONResponse: gen.ServiceUnavailableJSONResponse{
			Code: "service_unavailable", Message: "База данных недоступна",
		}}, nil
	}
	return gen.GetHealth200JSONResponse(h), nil
}

// publicOperations — операции со `security: []`: токен для них не нужен.
func publicOperations(swagger *openapi3.T) map[string]bool {
	public := map[string]bool{}
	for _, item := range swagger.Paths.Map() {
		for _, op := range item.Operations() {
			if op.Security != nil && len(*op.Security) == 0 {
				public[op.OperationID] = true
			}
		}
	}
	return public
}

// userStoreAdapter переводит типы модуля user в типы, которые объявил auth.
type userStoreAdapter struct{ svc *user.Service }

func (a userStoreAdapter) Create(ctx context.Context, u auth.NewUser) (auth.User, error) {
	acc, err := a.svc.Create(ctx, user.NewAccount{
		Login: u.Login, PasswordHash: u.PasswordHash,
		FirstName: u.FirstName, MiddleName: u.MiddleName,
		LastName: u.LastName, Link: u.Link,
	})
	if err != nil {
		return auth.User{}, mapUserErr("Create", err)
	}
	return toAuthUser(acc.User), nil
}

func (a userStoreAdapter) GetByLogin(ctx context.Context, login string) (auth.UserWithHash, error) {
	acc, err := a.svc.GetByLogin(ctx, login)
	if err != nil {
		return auth.UserWithHash{}, mapUserErr("GetByLogin", err)
	}
	return auth.UserWithHash{User: toAuthUser(acc.User), PasswordHash: acc.PasswordHash}, nil
}

func (a userStoreAdapter) GetByID(ctx context.Context, id int64) (auth.User, error) {
	acc, err := a.svc.GetByID(ctx, id)
	if err != nil {
		return auth.User{}, mapUserErr("GetByID", err)
	}
	return toAuthUser(acc.User), nil
}

func toAuthUser(u user.User) auth.User {
	return auth.User{
		ID: u.ID, Login: u.Login, FirstName: u.FirstName, MiddleName: u.MiddleName,
		LastName: u.LastName, Link: u.Link, CreatedAt: u.CreatedAt,
	}
}

// mapUserErr переводит ошибки user в ошибки auth, сохраняя исходную цепочку.
func mapUserErr(op string, err error) error {
	switch {
	case errors.Is(err, user.ErrLoginTaken):
		return fmt.Errorf("userStoreAdapter.%s: %w (%w)", op, auth.ErrLoginTaken, err)
	case errors.Is(err, user.ErrNotFound):
		return fmt.Errorf("userStoreAdapter.%s: %w (%w)", op, auth.ErrUserNotFound, err)
	default:
		return fmt.Errorf("userStoreAdapter.%s: %w", op, err)
	}
}

// memberSplitReader отдаёт модулю member сведения о пространстве. svc
// проставляется после создания split.Service: split и member нужны друг другу
// при сборке, хотя пакеты друг друга не импортируют.
type memberSplitReader struct{ svc *split.Service }

func (a *memberSplitReader) SplitInfo(ctx context.Context, splitID int64) (member.SplitInfo, error) {
	sp, err := a.svc.Info(ctx, splitID)
	if errors.Is(err, split.ErrNotFound) {
		return member.SplitInfo{}, fmt.Errorf("memberSplitReader.SplitInfo: %w (%w)", member.ErrSplitNotFound, err)
	}
	if err != nil {
		return member.SplitInfo{}, fmt.Errorf("memberSplitReader.SplitInfo: %w", err)
	}
	return member.SplitInfo{OwnerID: sp.OwnerID, Status: string(sp.Status), MaxMembers: sp.MaxMembers}, nil
}

// inviteRedeemerAdapter переводит invite.Service в порт InviteRedeemer; svc
// проставляется при сборке в main (см. memberSplitReader).
type inviteRedeemerAdapter struct{ svc *invite.Service }

func (a *inviteRedeemerAdapter) Redeem(ctx context.Context, token string) (member.Redemption, error) {
	r, err := a.svc.Redeem(ctx, token)
	if errors.Is(err, invite.ErrInviteInvalid) {
		return member.Redemption{}, fmt.Errorf("inviteRedeemerAdapter.Redeem: %w (%w)", member.ErrInviteInvalid, err)
	}
	if err != nil {
		return member.Redemption{}, fmt.Errorf("inviteRedeemerAdapter.Redeem: %w", err)
	}
	return member.Redemption{InviteID: r.InviteID, SplitID: r.SplitID}, nil
}

func (a *inviteRedeemerAdapter) Release(ctx context.Context, inviteID int64) error {
	if err := a.svc.Release(ctx, inviteID); err != nil {
		return fmt.Errorf("inviteRedeemerAdapter.Release: %w", err)
	}
	return nil
}

// formSnapshotAdapter переводит form.Service в порт FormSnapshotter.
type formSnapshotAdapter struct{ svc *form.Service }

func (a formSnapshotAdapter) Snapshot(ctx context.Context, userID, formID int64) (member.FormSnapshot, error) {
	sn, err := a.svc.Snapshot(ctx, userID, formID)
	if errors.Is(err, form.ErrNotFound) {
		return member.FormSnapshot{}, fmt.Errorf("formSnapshotAdapter.Snapshot: %w (%w)", member.ErrFormNotFound, err)
	}
	if err != nil {
		return member.FormSnapshot{}, fmt.Errorf("formSnapshotAdapter.Snapshot: %w", err)
	}
	prefs := make([]member.RolePreference, len(sn.RolePreferences))
	for i, p := range sn.RolePreferences {
		prefs[i] = member.RolePreference{RoleName: p.RoleName, Rank: p.Rank}
	}
	return member.FormSnapshot{
		Experience: sn.Experience, Skills: sn.Skills, RolePreferences: prefs, Comment: sn.Comment,
	}, nil
}

// userDirectoryAdapter переводит user.Service в порт UserDirectory.
type userDirectoryAdapter struct{ svc *user.Service }

func (a userDirectoryAdapter) ListBriefs(ctx context.Context, ids []int64) ([]member.UserBrief, error) {
	users, err := a.svc.ListBriefs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("userDirectoryAdapter.ListBriefs: %w", err)
	}
	out := make([]member.UserBrief, len(users))
	for i, u := range users {
		out[i] = member.UserBrief{ID: u.ID, FirstName: u.FirstName, LastName: u.LastName, Link: u.Link}
	}
	return out, nil
}

// hubPublisher публикует смену состава в SSE-поток пространства.
type hubPublisher struct{ hub *split.Hub }

func (p hubPublisher) MembersChanged(splitID int64, membersCount int) {
	p.hub.Publish(splitID, split.NewEvent("member_joined", map[string]int{"membersCount": membersCount}))
}

// splitMembershipAdapter отдаёт модулю split участников и счётчики из member.
// Число команд даст модуль team, пока оно nil.
type splitMembershipAdapter struct{ svc *member.Service }

func (a splitMembershipAdapter) IsMember(ctx context.Context, splitID, userID int64) (bool, error) {
	return a.svc.IsMember(ctx, splitID, userID)
}

func (a splitMembershipAdapter) SplitIDsOfUser(ctx context.Context, userID int64) ([]int64, error) {
	return a.svc.SplitIDsOfUser(ctx, userID)
}

func (a splitMembershipAdapter) Stats(ctx context.Context, splitIDs []int64) (map[int64]split.Stats, error) {
	counts, err := a.svc.Counts(ctx, splitIDs)
	if err != nil {
		return nil, fmt.Errorf("splitMembershipAdapter.Stats: %w", err)
	}
	out := make(map[int64]split.Stats, len(counts))
	for id, n := range counts {
		out[id] = split.Stats{MembersCount: n}
	}
	return out, nil
}

// splitAccessAdapter переводит split.Service в порт SplitAccess модуля invite.
type splitAccessAdapter struct{ svc *split.Service }

func (a splitAccessAdapter) Authorize(ctx context.Context, userID, splitID int64) (invite.Access, error) {
	acc, err := a.svc.Authorize(ctx, userID, splitID)
	if errors.Is(err, split.ErrNotFound) {
		return "", fmt.Errorf("splitAccessAdapter.Authorize: %w (%w)", invite.ErrSplitNotFound, err)
	}
	if err != nil {
		return "", fmt.Errorf("splitAccessAdapter.Authorize: %w", err)
	}
	if acc == split.AccessOwner {
		return invite.AccessOwner, nil
	}
	return invite.AccessMember, nil
}

// splitInfoAdapter отдаёт модулю invite публичные сведения о пространстве.
type splitInfoAdapter struct{ svc *split.Service }

func (a splitInfoAdapter) SplitInfo(ctx context.Context, splitID int64) (invite.SplitInfo, error) {
	sp, err := a.svc.Info(ctx, splitID)
	if errors.Is(err, split.ErrNotFound) {
		return invite.SplitInfo{}, fmt.Errorf("splitInfoAdapter.SplitInfo: %w (%w)", invite.ErrSplitNotFound, err)
	}
	if err != nil {
		return invite.SplitInfo{}, fmt.Errorf("splitInfoAdapter.SplitInfo: %w", err)
	}
	names := make([]string, len(sp.Roles))
	for i, r := range sp.Roles {
		names[i] = r.Name
	}
	return invite.SplitInfo{
		Title: sp.Title, Description: sp.Description, TeamSize: sp.TeamSize,
		RoleNames: names, MembersCount: sp.MembersCount, Status: string(sp.Status),
	}, nil
}
