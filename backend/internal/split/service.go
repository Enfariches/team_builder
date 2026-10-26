package split

import (
	"context"
	"fmt"
)

// Store — операции с таблицами пространств; в транзакции получает pgx.Tx.
type Store interface {
	Insert(ctx context.Context, n NewSplit) (Split, error)
	Get(ctx context.Context, id int64) (Split, error) // ErrNotFound
	GetForUpdate(ctx context.Context, id int64) (Split, error)
	Update(ctx context.Context, id int64, p Patch) (Split, error)
	SetStatus(ctx context.Context, id int64, status Status) (Split, error)
	Delete(ctx context.Context, id int64) error
	ListForUser(ctx context.Context, userID int64, memberIDs []int64, role *string, status *Status) ([]Split, error)
}

// TxRunner открывает транзакцию по запросу сервиса: границу определяет
// service, репозиторий получает готовое соединение.
type TxRunner interface {
	Run(ctx context.Context, fn func(Store) error) error
}

type Service struct {
	store   Store
	tx      TxRunner
	members MembershipProvider
	stats   StatsProvider
	hub     *Hub
}

func NewService(store Store, tx TxRunner, members MembershipProvider, stats StatsProvider, hub *Hub) *Service {
	return &Service{store: store, tx: tx, members: members, stats: stats, hub: hub}
}

func (s *Service) Create(ctx context.Context, userID int64, in NewSplit) (Split, error) {
	in.OwnerID = userID
	in = in.Normalize()
	if err := in.Validate(); err != nil {
		return Split{}, fmt.Errorf("split.Service.Create: validate: %w", err)
	}
	var sp Split
	err := s.tx.Run(ctx, func(st Store) error {
		var err error
		sp, err = st.Insert(ctx, in)
		return err
	})
	if err != nil {
		return Split{}, fmt.Errorf("split.Service.Create: %w", err)
	}
	sp.MyRole = AccessOwner
	return sp, nil
}

func (s *Service) Get(ctx context.Context, userID, id int64) (Split, error) {
	sp, err := s.store.Get(ctx, id)
	if err != nil {
		return Split{}, fmt.Errorf("split.Service.Get: split %d: %w", id, err)
	}
	acc, err := s.accessOf(ctx, userID, sp)
	if err != nil {
		return Split{}, fmt.Errorf("split.Service.Get: split %d: %w", id, err)
	}
	sp.MyRole = acc
	if err := s.fillStats(ctx, []Split{sp}, func(i int, st Stats) { applyStats(&sp, st) }); err != nil {
		return Split{}, fmt.Errorf("split.Service.Get: %w", err)
	}
	return sp, nil
}

// Authorize проверяет, что пользователь владелец или участник, и возвращает
// его роль. Для потока событий этого достаточно.
func (s *Service) Authorize(ctx context.Context, userID, id int64) (Access, error) {
	sp, err := s.store.Get(ctx, id)
	if err != nil {
		return "", fmt.Errorf("split.Service.Authorize: split %d: %w", id, err)
	}
	acc, err := s.accessOf(ctx, userID, sp)
	if err != nil {
		return "", fmt.Errorf("split.Service.Authorize: split %d: %w", id, err)
	}
	return acc, nil
}

func (s *Service) List(ctx context.Context, userID int64, role *string, status *Status) ([]Split, error) {
	memberIDs, err := s.members.SplitIDsOfUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("split.Service.List: member splits: %w", err)
	}
	list, err := s.store.ListForUser(ctx, userID, memberIDs, role, status)
	if err != nil {
		return nil, fmt.Errorf("split.Service.List: %w", err)
	}
	for i := range list {
		if list[i].OwnerID == userID {
			list[i].MyRole = AccessOwner
		} else {
			list[i].MyRole = AccessMember
		}
	}
	if err := s.fillStats(ctx, list, func(i int, st Stats) { applyStats(&list[i], st) }); err != nil {
		return nil, fmt.Errorf("split.Service.List: %w", err)
	}
	return list, nil
}

// Update, Delete и ChangeStatus сначала проверяют доступ и берут счётчики из
// портов, и только потом открывают транзакцию. Внутри транзакции нельзя
// обращаться к портам: реализация порта может ходить в тот же пул соединений,
// и при большом числе параллельных запросов пул исчерпается взаимной
// блокировкой (каждая транзакция держит соединение и ждёт второе).
// Владелец пространства не меняется, поэтому проверка до блокировки верна.

func (s *Service) Update(ctx context.Context, userID, id int64, p Patch) (Split, error) {
	p = p.Normalize()
	if err := s.checkOwner(ctx, userID, id); err != nil {
		return Split{}, fmt.Errorf("split.Service.Update: split %d: %w", id, err)
	}
	stats, err := s.statsOf(ctx, id)
	if err != nil {
		return Split{}, fmt.Errorf("split.Service.Update: split %d: %w", id, err)
	}

	var sp Split
	err = s.tx.Run(ctx, func(st Store) error {
		cur, err := st.GetForUpdate(ctx, id)
		if err != nil {
			return fmt.Errorf("lock split: %w", err)
		}
		if cur.Status == StatusFinished {
			return ErrSplitFinished
		}
		if cur.Status != StatusDraft && p.ChangesRules(cur) {
			return ErrRulesLocked
		}
		if err := p.Validate(cur, stats.MembersCount); err != nil {
			return fmt.Errorf("validate patch: %w", err)
		}
		sp, err = st.Update(ctx, id, p)
		if err != nil {
			return fmt.Errorf("update: %w", err)
		}
		return nil
	})
	if err != nil {
		return Split{}, fmt.Errorf("split.Service.Update: split %d: %w", id, err)
	}
	applyStats(&sp, stats)
	sp.MyRole = AccessOwner
	return sp, nil
}

func (s *Service) Delete(ctx context.Context, userID, id int64) error {
	if err := s.checkOwner(ctx, userID, id); err != nil {
		return fmt.Errorf("split.Service.Delete: split %d: %w", id, err)
	}
	err := s.tx.Run(ctx, func(st Store) error {
		if _, err := st.GetForUpdate(ctx, id); err != nil {
			return fmt.Errorf("lock split: %w", err)
		}
		if err := st.Delete(ctx, id); err != nil {
			return fmt.Errorf("delete: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("split.Service.Delete: split %d: %w", id, err)
	}
	s.hub.CloseSplit(id)
	return nil
}

func (s *Service) ChangeStatus(ctx context.Context, userID, id int64, to Status) (Split, error) {
	if err := s.checkOwner(ctx, userID, id); err != nil {
		return Split{}, fmt.Errorf("split.Service.ChangeStatus: split %d: %w", id, err)
	}
	var sp Split
	err := s.tx.Run(ctx, func(st Store) error {
		cur, err := st.GetForUpdate(ctx, id)
		if err != nil {
			return fmt.Errorf("lock split: %w", err)
		}
		if !to.Valid() || !cur.Status.CanTransition(to) {
			return &TransitionError{From: cur.Status, To: to}
		}
		sp, err = st.SetStatus(ctx, id, to)
		if err != nil {
			return fmt.Errorf("set status: %w", err)
		}
		return nil
	})
	if err != nil {
		return Split{}, fmt.Errorf("split.Service.ChangeStatus: split %d: %w", id, err)
	}
	// событие уходит только после коммита, иначе подписчик увидит откатённое
	s.hub.Publish(id, NewEvent("status_changed", map[string]string{"status": string(sp.Status)}))

	stats, err := s.statsOf(ctx, id)
	if err != nil {
		return Split{}, fmt.Errorf("split.Service.ChangeStatus: %w", err)
	}
	applyStats(&sp, stats)
	sp.MyRole = AccessOwner
	return sp, nil
}

// checkOwner читает пространство вне транзакции и требует права владельца.
func (s *Service) checkOwner(ctx context.Context, userID, id int64) error {
	sp, err := s.store.Get(ctx, id)
	if err != nil {
		return err
	}
	return s.requireOwner(ctx, userID, sp)
}

// accessOf: владелец, участник или ErrNotFound (наличие пространства
// постороннему не раскрывается).
func (s *Service) accessOf(ctx context.Context, userID int64, sp Split) (Access, error) {
	if sp.OwnerID == userID {
		return AccessOwner, nil
	}
	ok, err := s.members.IsMember(ctx, sp.ID, userID)
	if err != nil {
		return "", fmt.Errorf("check membership: %w", err)
	}
	if ok {
		return AccessMember, nil
	}
	return "", ErrNotFound
}

func (s *Service) requireOwner(ctx context.Context, userID int64, sp Split) error {
	acc, err := s.accessOf(ctx, userID, sp)
	if err != nil {
		return err
	}
	if acc != AccessOwner {
		return ErrForbidden
	}
	return nil
}

func (s *Service) statsOf(ctx context.Context, id int64) (Stats, error) {
	m, err := s.stats.Stats(ctx, []int64{id})
	if err != nil {
		return Stats{}, fmt.Errorf("stats: %w", err)
	}
	return m[id], nil
}

func (s *Service) fillStats(ctx context.Context, list []Split, apply func(i int, st Stats)) error {
	if len(list) == 0 {
		return nil
	}
	ids := make([]int64, len(list))
	for i, sp := range list {
		ids[i] = sp.ID
	}
	m, err := s.stats.Stats(ctx, ids)
	if err != nil {
		return fmt.Errorf("stats: %w", err)
	}
	for i, sp := range list {
		apply(i, m[sp.ID])
	}
	return nil
}

func applyStats(sp *Split, st Stats) {
	sp.MembersCount = st.MembersCount
	sp.TeamsCount = st.TeamsCount
}
