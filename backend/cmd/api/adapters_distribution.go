package main

import (
	"context"
	"errors"
	"fmt"

	"builder/backend/internal/distribution"
	"builder/backend/internal/member"
	"builder/backend/internal/split"
	"builder/backend/internal/team"
	"builder/backend/internal/user"
)

// Адаптеры модулей distribution и team. Типы на границе принадлежат
// потребителю, поэтому здесь только перевод из типов поставщика.

// distSplitGate переводит split.Service в порт SplitGate модуля distribution.
type distSplitGate struct{ svc *split.Service }

func (a distSplitGate) State(ctx context.Context, splitID int64) (distribution.SplitState, error) {
	sp, err := a.svc.Lookup(ctx, splitID)
	return a.state("State", sp, err)
}

func (a distSplitGate) LockForPublish(ctx context.Context, splitID int64) (distribution.SplitState, error) {
	sp, err := a.svc.LockForPublish(ctx, splitID)
	return a.state("LockForPublish", sp, err)
}

func (a distSplitGate) MarkFinished(ctx context.Context, splitID int64) error {
	if err := a.svc.MarkFinished(ctx, splitID); err != nil {
		return fmt.Errorf("distSplitGate.MarkFinished: %w", err)
	}
	return nil
}

func (distSplitGate) state(op string, sp split.Split, err error) (distribution.SplitState, error) {
	if errors.Is(err, split.ErrNotFound) {
		return distribution.SplitState{}, fmt.Errorf("distSplitGate.%s: %w (%w)", op, distribution.ErrSplitNotFound, err)
	}
	if err != nil {
		return distribution.SplitState{}, fmt.Errorf("distSplitGate.%s: %w", op, err)
	}
	roles := make([]distribution.Role, len(sp.Roles))
	for i, r := range sp.Roles {
		roles[i] = distribution.Role{Name: r.Name, Min: r.PerTeamMin, Max: r.PerTeamMax, SortOrder: r.SortOrder}
	}
	return distribution.SplitState{
		OwnerID: sp.OwnerID,
		Status:  string(sp.Status),
		Rules: distribution.Rules{
			TeamSize: sp.TeamSize, Roles: roles, Policy: distribution.RemainderPolicy(sp.RemainderPolicy),
		},
	}, nil
}

// distRoster переводит member.Service в порт Roster модуля distribution.
type distRoster struct{ svc *member.Service }

func (a distRoster) Lock(ctx context.Context, splitID int64) error {
	return a.svc.LockRoster(ctx, splitID)
}

func (a distRoster) IsMember(ctx context.Context, splitID, userID int64) (bool, error) {
	return a.svc.IsMember(ctx, splitID, userID)
}

func (a distRoster) Candidates(ctx context.Context, splitID int64) ([]distribution.Candidate, error) {
	list, err := a.svc.Candidates(ctx, splitID)
	if err != nil {
		return nil, fmt.Errorf("distRoster.Candidates: %w", err)
	}
	out := make([]distribution.Candidate, len(list))
	for i, m := range list {
		prefs := make([]distribution.RolePreference, len(m.Snapshot.RolePreferences))
		for j, p := range m.Snapshot.RolePreferences {
			prefs[j] = distribution.RolePreference{Role: p.RoleName, Rank: p.Rank}
		}
		out[i] = distribution.Candidate{
			MemberID: m.ID, UserID: m.UserID, JoinedAt: m.JoinedAt,
			Experience: m.Snapshot.Experience, Preferences: prefs,
		}
	}
	return out, nil
}

func (a distRoster) Assign(ctx context.Context, splitID int64, ps []distribution.Placement) error {
	out := make([]member.Placement, len(ps))
	for i, p := range ps {
		out[i] = member.Placement{MemberID: p.MemberID, TeamID: p.TeamID, Role: p.Role}
	}
	return a.svc.Assign(ctx, splitID, out)
}

// distUserDirectory переводит user.Service в порт UserDirectory модуля distribution.
type distUserDirectory struct{ svc *user.Service }

func (a distUserDirectory) ListBriefs(ctx context.Context, ids []int64) ([]distribution.UserBrief, error) {
	users, err := a.svc.ListBriefs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("distUserDirectory.ListBriefs: %w", err)
	}
	out := make([]distribution.UserBrief, len(users))
	for i, u := range users {
		out[i] = distribution.UserBrief{ID: u.ID, FirstName: u.FirstName, LastName: u.LastName, Link: u.Link}
	}
	return out, nil
}

// distEvents сообщает подписчикам потока пространства о публикации: сам факт
// и новый статус, чтобы клиенту не пришлось перечитывать пространство.
type distEvents struct{ hub *split.Hub }

func (e distEvents) Published(splitID, runID int64) {
	e.hub.Publish(splitID, split.NewEvent("distribution_published", map[string]int64{"runId": runID}))
	e.hub.Publish(splitID, split.NewEvent("status_changed", map[string]string{"status": string(split.StatusFinished)}))
}

// teamSplitReader переводит split.Service в порт SplitReader модуля team.
type teamSplitReader struct{ svc *split.Service }

func (a teamSplitReader) State(ctx context.Context, splitID int64) (team.SplitState, error) {
	sp, err := a.svc.Lookup(ctx, splitID)
	if errors.Is(err, split.ErrNotFound) {
		return team.SplitState{}, fmt.Errorf("teamSplitReader.State: %w (%w)", team.ErrSplitNotFound, err)
	}
	if err != nil {
		return team.SplitState{}, fmt.Errorf("teamSplitReader.State: %w", err)
	}
	return team.SplitState{OwnerID: sp.OwnerID, Status: string(sp.Status)}, nil
}

// teamMemberDirectory переводит member.Service в порт MemberDirectory модуля team.
type teamMemberDirectory struct{ svc *member.Service }

func (a teamMemberDirectory) IsMember(ctx context.Context, splitID, userID int64) (bool, error) {
	return a.svc.IsMember(ctx, splitID, userID)
}

func (a teamMemberDirectory) ListAssignments(ctx context.Context, splitID int64) ([]team.Assignment, error) {
	list, err := a.svc.Assignments(ctx, splitID)
	if err != nil {
		return nil, fmt.Errorf("teamMemberDirectory.ListAssignments: %w", err)
	}
	out := make([]team.Assignment, len(list))
	for i, m := range list {
		out[i] = team.Assignment{MemberID: m.MemberID, UserID: m.UserID, TeamID: m.TeamID, Role: m.Role}
	}
	return out, nil
}

// teamUserDirectory переводит user.Service в порт UserDirectory модуля team.
type teamUserDirectory struct{ svc *user.Service }

func (a teamUserDirectory) ListBriefs(ctx context.Context, ids []int64) ([]team.UserBrief, error) {
	users, err := a.svc.ListBriefs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("teamUserDirectory.ListBriefs: %w", err)
	}
	out := make([]team.UserBrief, len(users))
	for i, u := range users {
		out[i] = team.UserBrief{ID: u.ID, FirstName: u.FirstName, LastName: u.LastName, Link: u.Link}
	}
	return out, nil
}
