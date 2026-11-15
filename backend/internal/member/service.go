package member

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Store — то, что сервису нужно от репозитория. Get возвращает ErrNotFound.
type Store interface {
	LockSplit(ctx context.Context, splitID int64) error
	Insert(ctx context.Context, n NewMember) (Member, error)
	Get(ctx context.Context, id int64) (Member, error)
	GetByUser(ctx context.Context, splitID, userID int64) (Member, error)
	List(ctx context.Context, splitID int64) ([]Member, error)
	Count(ctx context.Context, splitID int64) (int, error)
	Counts(ctx context.Context, splitIDs []int64) (map[int64]int, error)
	SplitIDsOfUser(ctx context.Context, userID int64) ([]int64, error)
	IsMember(ctx context.Context, splitID, userID int64) (bool, error)
	Delete(ctx context.Context, id int64) error
	Assign(ctx context.Context, splitID int64, placements []Placement) error
	ListAssignments(ctx context.Context, splitID int64) ([]Assignment, error)
}

// TxRunner выполняет fn в одной транзакции; Store внутри привязан к ней.
type TxRunner interface {
	Run(ctx context.Context, fn func(Store) error) error
}

type Service struct {
	store   Store
	tx      TxRunner
	invites InviteRedeemer
	forms   FormSnapshotter
	splits  SplitReader
	users   UserDirectory
	events  EventPublisher
	locks   keyedMutex
}

func NewService(store Store, tx TxRunner, invites InviteRedeemer, forms FormSnapshotter,
	splits SplitReader, users UserDirectory, events EventPublisher) *Service {
	return &Service{store: store, tx: tx, invites: invites, forms: forms,
		splits: splits, users: users, events: events}
}

// withSplitLock выполняет fn в транзакции под advisory-lock пространства.
//
// Сначала берётся мьютекс в памяти процесса: внутри транзакции мы читаем
// сведения о пространстве через пул (второе соединение при уже занятом
// первом). Если бы все ожидающие вступления держали по соединению в очереди
// за advisory-lock, пул опустел бы и держатель lock'а не получил бы второго
// соединения. Мьютекс пускает в БД по одному вступлению на пространство;
// advisory-lock остаётся для других экземпляров и для publish.
func (s *Service) withSplitLock(ctx context.Context, splitID int64, fn func(Store) error) error {
	unlock, err := s.locks.lock(ctx, splitID)
	if err != nil {
		return fmt.Errorf("wait split lock: %w", err)
	}
	defer unlock()
	return s.tx.Run(ctx, func(st Store) error {
		if err := st.LockSplit(ctx, splitID); err != nil {
			return fmt.Errorf("lock split: %w", err)
		}
		return fn(st)
	})
}

// Join вступает в пространство по инвайту, заморозив анкету в снимок.
//
// Использование инвайта занимается до транзакции, а при любой неудаче
// возвращается: так лимит инвайта не расходуется на отказы. Проверки статуса,
// мест и повтора идут под advisory-lock пространства, поэтому параллельные
// вступления не превысят maxMembers.
func (s *Service) Join(ctx context.Context, userID int64, token string, formID int64) (Member, error) {
	// анкета проверяется первой: чужая анкета не должна тратить использование инвайта
	snap, err := s.forms.Snapshot(ctx, userID, formID)
	if err != nil {
		return Member{}, fmt.Errorf("member.Service.Join: snapshot: %w", err)
	}
	red, err := s.invites.Redeem(ctx, token)
	if err != nil {
		return Member{}, fmt.Errorf("member.Service.Join: redeem: %w", err)
	}

	m, count, err := s.insert(ctx, userID, red.SplitID, formID, snap)
	if err != nil {
		// WithoutCancel: отмена запроса не должна оставить использование занятым
		if rerr := s.invites.Release(context.WithoutCancel(ctx), red.InviteID); rerr != nil {
			err = errors.Join(err, fmt.Errorf("release invite %d: %w", red.InviteID, rerr))
		}
		return Member{}, fmt.Errorf("member.Service.Join: %w", err)
	}
	s.events.MembersChanged(red.SplitID, count)

	one := []Member{m}
	if err := s.fillUsers(ctx, one); err != nil {
		return Member{}, fmt.Errorf("member.Service.Join: %w", err)
	}
	return one[0], nil
}

func (s *Service) insert(ctx context.Context, userID, splitID, formID int64, snap FormSnapshot) (Member, int, error) {
	var (
		m     Member
		count int
	)
	err := s.withSplitLock(ctx, splitID, func(st Store) error {
		info, err := s.splits.SplitInfo(ctx, splitID)
		if err != nil {
			return fmt.Errorf("split info: %w", err)
		}
		already, err := st.IsMember(ctx, splitID, userID)
		if err != nil {
			return fmt.Errorf("check membership: %w", err)
		}
		n, err := st.Count(ctx, splitID)
		if err != nil {
			return fmt.Errorf("count members: %w", err)
		}
		if err := CheckCanJoin(info, userID, already, n); err != nil {
			return err
		}
		m, err = st.Insert(ctx, NewMember{SplitID: splitID, UserID: userID, FormID: formID, Snapshot: snap})
		if err != nil {
			return fmt.Errorf("insert member: %w", err)
		}
		count = n + 1
		return nil
	})
	return m, count, err
}

// ListBySplit — список с анкетами, только владельцу (приватность анкет).
func (s *Service) ListBySplit(ctx context.Context, userID, splitID int64) ([]Member, error) {
	if err := s.requireOwner(ctx, userID, splitID); err != nil {
		return nil, fmt.Errorf("member.Service.ListBySplit: %w", err)
	}
	list, err := s.store.List(ctx, splitID)
	if err != nil {
		return nil, fmt.Errorf("member.Service.ListBySplit: %w", err)
	}
	if err := s.fillUsers(ctx, list); err != nil {
		return nil, fmt.Errorf("member.Service.ListBySplit: %w", err)
	}
	return list, nil
}

func (s *Service) GetMine(ctx context.Context, userID, splitID int64) (Member, error) {
	m, err := s.store.GetByUser(ctx, splitID, userID)
	if err != nil {
		return Member{}, fmt.Errorf("member.Service.GetMine: %w", err)
	}
	one := []Member{m}
	if err := s.fillUsers(ctx, one); err != nil {
		return Member{}, fmt.Errorf("member.Service.GetMine: %w", err)
	}
	return one[0], nil
}

// Leave — выход участника, пока распределение не опубликовано.
func (s *Service) Leave(ctx context.Context, userID, splitID int64) error {
	var count int
	err := s.withSplitLock(ctx, splitID, func(st Store) error {
		m, err := st.GetByUser(ctx, splitID, userID)
		if err != nil {
			return err
		}
		n, err := s.removeLocked(ctx, st, m)
		count = n
		return err
	})
	if err != nil {
		return fmt.Errorf("member.Service.Leave: %w", err)
	}
	s.events.MembersChanged(splitID, count)
	return nil
}

// Remove — исключение участника владельцем. Чужое пространство даёт 404,
// участник того же пространства — 403: ему факт существования уже известен.
func (s *Service) Remove(ctx context.Context, userID, memberID int64) error {
	target, err := s.store.Get(ctx, memberID)
	if err != nil {
		return fmt.Errorf("member.Service.Remove: %w", err)
	}
	if err := s.requireOwner(ctx, userID, target.SplitID); err != nil {
		if errors.Is(err, ErrSplitNotFound) {
			err = ErrNotFound
		}
		return fmt.Errorf("member.Service.Remove: %w", err)
	}

	var count int
	err = s.withSplitLock(ctx, target.SplitID, func(st Store) error {
		// участник мог выйти, пока мы ждали lock
		m, err := st.Get(ctx, memberID)
		if err != nil {
			return err
		}
		n, err := s.removeLocked(ctx, st, m)
		count = n
		return err
	})
	if err != nil {
		return fmt.Errorf("member.Service.Remove: %w", err)
	}
	s.events.MembersChanged(target.SplitID, count)
	return nil
}

// removeLocked удаляет участника; вызывать под lock пространства.
// Возвращает число участников после удаления.
func (s *Service) removeLocked(ctx context.Context, st Store, m Member) (int, error) {
	info, err := s.splits.SplitInfo(ctx, m.SplitID)
	if err != nil {
		return 0, fmt.Errorf("split info: %w", err)
	}
	if info.Status == StatusFinished {
		return 0, ErrSplitFinished
	}
	if err := st.Delete(ctx, m.ID); err != nil {
		return 0, fmt.Errorf("delete member: %w", err)
	}
	n, err := st.Count(ctx, m.SplitID)
	if err != nil {
		return 0, fmt.Errorf("count members: %w", err)
	}
	return n, nil
}

// requireOwner: владелец — nil, участник — ErrForbidden, остальные — ErrSplitNotFound.
func (s *Service) requireOwner(ctx context.Context, userID, splitID int64) error {
	info, err := s.splits.SplitInfo(ctx, splitID)
	if err != nil {
		return fmt.Errorf("split info: %w", err)
	}
	if info.OwnerID == userID {
		return nil
	}
	ok, err := s.store.IsMember(ctx, splitID, userID)
	if err != nil {
		return fmt.Errorf("check membership: %w", err)
	}
	if ok {
		return ErrForbidden
	}
	return ErrSplitNotFound
}

// fillUsers проставляет UserBrief одним запросом на весь список.
func (s *Service) fillUsers(ctx context.Context, list []Member) error {
	if len(list) == 0 {
		return nil
	}
	ids := make([]int64, len(list))
	for i, m := range list {
		ids[i] = m.UserID
	}
	briefs, err := s.users.ListBriefs(ctx, ids)
	if err != nil {
		return fmt.Errorf("list user briefs: %w", err)
	}
	byID := make(map[int64]UserBrief, len(briefs))
	for _, b := range briefs {
		byID[b.ID] = b
	}
	for i := range list {
		list[i].User = byID[list[i].UserID]
	}
	return nil
}

// IsMember, SplitIDsOfUser и Counts отдают данные модулям split и distribution
// через их порты; адаптеры собираются в cmd/api.
func (s *Service) IsMember(ctx context.Context, splitID, userID int64) (bool, error) {
	ok, err := s.store.IsMember(ctx, splitID, userID)
	if err != nil {
		return false, fmt.Errorf("member.Service.IsMember: %w", err)
	}
	return ok, nil
}

func (s *Service) SplitIDsOfUser(ctx context.Context, userID int64) ([]int64, error) {
	ids, err := s.store.SplitIDsOfUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("member.Service.SplitIDsOfUser: %w", err)
	}
	return ids, nil
}

// Counts возвращает число участников по пространствам; отсутствие ключа — ноль.
func (s *Service) Counts(ctx context.Context, splitIDs []int64) (map[int64]int, error) {
	m, err := s.store.Counts(ctx, splitIDs)
	if err != nil {
		return nil, fmt.Errorf("member.Service.Counts: %w", err)
	}
	return m, nil
}

// keyedMutex — мьютекс на ключ с ожиданием, прерываемым контекстом.
// Нулевое значение готово к работе.
type keyedMutex struct {
	mu sync.Mutex
	m  map[int64]*keyedEntry
}

type keyedEntry struct {
	ch   chan struct{}
	refs int
}

func (k *keyedMutex) lock(ctx context.Context, key int64) (func(), error) {
	k.mu.Lock()
	if k.m == nil {
		k.m = map[int64]*keyedEntry{}
	}
	e := k.m[key]
	if e == nil {
		e = &keyedEntry{ch: make(chan struct{}, 1)}
		k.m[key] = e
	}
	e.refs++
	k.mu.Unlock()

	select {
	case e.ch <- struct{}{}:
		return func() {
			<-e.ch
			k.release(key, e)
		}, nil
	case <-ctx.Done():
		k.release(key, e)
		return nil, ctx.Err()
	}
}

func (k *keyedMutex) release(key int64, e *keyedEntry) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if e.refs--; e.refs == 0 {
		delete(k.m, key)
	}
}

// LockRoster берёт advisory-lock пространства до конца транзакции из ctx — тот
// же, под которым идут вступления и выходы. Нужен публикации распределения:
// после неё состав уже не меняется.
func (s *Service) LockRoster(ctx context.Context, splitID int64) error {
	if err := s.store.LockSplit(ctx, splitID); err != nil {
		return fmt.Errorf("member.Service.LockRoster: %w", err)
	}
	return nil
}

// Candidates отдаёт участников со снимками анкет для распределения.
func (s *Service) Candidates(ctx context.Context, splitID int64) ([]Member, error) {
	list, err := s.store.List(ctx, splitID)
	if err != nil {
		return nil, fmt.Errorf("member.Service.Candidates: %w", err)
	}
	return list, nil
}

// Assign записывает команды и роли, назначенные распределением.
func (s *Service) Assign(ctx context.Context, splitID int64, placements []Placement) error {
	if err := s.store.Assign(ctx, splitID, placements); err != nil {
		return fmt.Errorf("member.Service.Assign: %w", err)
	}
	return nil
}

// Assignments отдаёт размещение участников без анкет (для модуля team).
func (s *Service) Assignments(ctx context.Context, splitID int64) ([]Assignment, error) {
	list, err := s.store.ListAssignments(ctx, splitID)
	if err != nil {
		return nil, fmt.Errorf("member.Service.Assignments: %w", err)
	}
	return list, nil
}
