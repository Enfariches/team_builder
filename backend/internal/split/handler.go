package split

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/oapi-codegen/nullable"

	"builder/backend/internal/api/gen"
)

// DefaultPingInterval — период keep-alive события в SSE-потоке.
const DefaultPingInterval = 20 * time.Second

type Handler struct {
	svc      *Service
	hub      *Hub
	identity IdentityProvider
	ping     time.Duration
}

func NewHandler(svc *Service, hub *Hub, identity IdentityProvider, ping time.Duration) *Handler {
	if ping <= 0 {
		ping = DefaultPingInterval
	}
	return &Handler{svc: svc, hub: hub, identity: identity, ping: ping}
}

// userID: middleware auth уже отсёк запросы без токена, поэтому отсутствие
// id в контексте означает ошибку сборки, а не клиента.
func (h *Handler) userID(ctx context.Context) (int64, error) {
	id, ok := h.identity.UserID(ctx)
	if !ok {
		return 0, fmt.Errorf("no authenticated user in context")
	}
	return id, nil
}

func (h *Handler) ListSplits(ctx context.Context, req gen.ListSplitsRequestObject) (gen.ListSplitsResponseObject, error) {
	uid, err := h.userID(ctx)
	if err != nil {
		return nil, fmt.Errorf("split.Handler.ListSplits: %w", err)
	}
	var role *string
	if req.Params.Role != nil {
		v := string(*req.Params.Role)
		role = &v
	}
	var status *Status
	if req.Params.Status != nil {
		v := Status(*req.Params.Status)
		if !v.Valid() {
			return nil, &ValidationError{Problems: []FieldError{{"status", "допустимы Draft, Open, Closed, Finished"}}}
		}
		status = &v
	}
	list, err := h.svc.List(ctx, uid, role, status)
	if err != nil {
		return nil, fmt.Errorf("split.Handler.ListSplits: %w", err)
	}
	out := make(gen.ListSplits200JSONResponse, len(list))
	for i, sp := range list {
		out[i] = toDTO(sp)
	}
	return out, nil
}

func (h *Handler) CreateSplit(ctx context.Context, req gen.CreateSplitRequestObject) (gen.CreateSplitResponseObject, error) {
	uid, err := h.userID(ctx)
	if err != nil {
		return nil, fmt.Errorf("split.Handler.CreateSplit: %w", err)
	}
	sp, err := h.svc.Create(ctx, uid, fromCreateDTO(*req.Body))
	if err != nil {
		return nil, fmt.Errorf("split.Handler.CreateSplit: %w", err)
	}
	return gen.CreateSplit201JSONResponse(toDTO(sp)), nil
}

func (h *Handler) GetSplit(ctx context.Context, req gen.GetSplitRequestObject) (gen.GetSplitResponseObject, error) {
	uid, err := h.userID(ctx)
	if err != nil {
		return nil, fmt.Errorf("split.Handler.GetSplit: %w", err)
	}
	sp, err := h.svc.Get(ctx, uid, req.SplitId)
	if err != nil {
		return nil, fmt.Errorf("split.Handler.GetSplit: %w", err)
	}
	return gen.GetSplit200JSONResponse(toDTO(sp)), nil
}

func (h *Handler) UpdateSplit(ctx context.Context, req gen.UpdateSplitRequestObject) (gen.UpdateSplitResponseObject, error) {
	uid, err := h.userID(ctx)
	if err != nil {
		return nil, fmt.Errorf("split.Handler.UpdateSplit: %w", err)
	}
	sp, err := h.svc.Update(ctx, uid, req.SplitId, fromUpdateDTO(*req.Body))
	if err != nil {
		return nil, fmt.Errorf("split.Handler.UpdateSplit: %w", err)
	}
	return gen.UpdateSplit200JSONResponse(toDTO(sp)), nil
}

func (h *Handler) DeleteSplit(ctx context.Context, req gen.DeleteSplitRequestObject) (gen.DeleteSplitResponseObject, error) {
	uid, err := h.userID(ctx)
	if err != nil {
		return nil, fmt.Errorf("split.Handler.DeleteSplit: %w", err)
	}
	if err := h.svc.Delete(ctx, uid, req.SplitId); err != nil {
		return nil, fmt.Errorf("split.Handler.DeleteSplit: %w", err)
	}
	return gen.DeleteSplit204Response{}, nil
}

func (h *Handler) ChangeSplitStatus(ctx context.Context, req gen.ChangeSplitStatusRequestObject) (gen.ChangeSplitStatusResponseObject, error) {
	uid, err := h.userID(ctx)
	if err != nil {
		return nil, fmt.Errorf("split.Handler.ChangeSplitStatus: %w", err)
	}
	sp, err := h.svc.ChangeStatus(ctx, uid, req.SplitId, Status(req.Body.Status))
	if err != nil {
		return nil, fmt.Errorf("split.Handler.ChangeSplitStatus: %w", err)
	}
	return gen.ChangeSplitStatus200JSONResponse(toDTO(sp)), nil
}

// StreamSplit проверяет доступ и отдаёт ответ, который сам ведёт SSE-поток:
// strict-обёртка не умеет стримить, поэтому запись и flush — в Visit-методе.
func (h *Handler) StreamSplit(ctx context.Context, req gen.StreamSplitRequestObject) (gen.StreamSplitResponseObject, error) {
	uid, err := h.userID(ctx)
	if err != nil {
		return nil, fmt.Errorf("split.Handler.StreamSplit: %w", err)
	}
	if _, err := h.svc.Authorize(ctx, uid, req.SplitId); err != nil {
		return nil, fmt.Errorf("split.Handler.StreamSplit: %w", err)
	}
	events, cancel := h.hub.Subscribe(req.SplitId)
	return sseResponse{ctx: ctx, events: events, cancel: cancel, ping: h.ping}, nil
}

type sseResponse struct {
	ctx    context.Context
	events <-chan Event
	cancel func()
	ping   time.Duration
}

func (r sseResponse) VisitStreamSplitResponse(w http.ResponseWriter) error {
	defer r.cancel()

	hd := w.Header()
	hd.Set("Content-Type", "text/event-stream")
	hd.Set("Cache-Control", "no-cache")
	hd.Set("Connection", "keep-alive")
	hd.Set("X-Accel-Buffering", "no") // прокси не должен буферизовать поток
	w.WriteHeader(http.StatusOK)

	rc := http.NewResponseController(w)
	write := func(format string, args ...any) bool {
		if _, err := fmt.Fprintf(w, format, args...); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	if !write(": connected\n\n") {
		return nil
	}

	ticker := time.NewTicker(r.ping)
	defer ticker.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return nil
		case ev, ok := <-r.events:
			if !ok {
				return nil
			}
			if !write("event: %s\ndata: %s\n\n", ev.Type, ev.Data) {
				return nil
			}
		case <-ticker.C:
			if !write("event: ping\ndata: {}\n\n") {
				return nil
			}
		}
	}
}

func toDTO(sp Split) gen.Split {
	roles := make([]gen.SplitRole, len(sp.Roles))
	for i, r := range sp.Roles {
		id, order := r.ID, r.SortOrder
		roles[i] = gen.SplitRole{
			Id: &id, Name: r.Name, PerTeamMin: r.PerTeamMin,
			PerTeamMax: r.PerTeamMax, SortOrder: &order,
		}
	}
	status := gen.SplitStatus(sp.Status)
	policy := gen.RemainderPolicy(sp.RemainderPolicy)
	myRole := gen.SplitMyRole(sp.MyRole)
	return gen.Split{
		Id: &sp.ID, Title: &sp.Title, Description: sp.Description, Status: &status,
		TeamSize: &sp.TeamSize, MaxMembers: sp.MaxMembers, RemainderPolicy: &policy,
		Roles: &roles, AutoFinishAt: sp.AutoFinishAt, MembersCount: &sp.MembersCount,
		TeamsCount: sp.TeamsCount, OwnerId: &sp.OwnerID, MyRole: &myRole,
		CreatedAt: &sp.CreatedAt, UpdatedAt: &sp.UpdatedAt,
	}
}

func fromCreateDTO(b gen.SplitCreate) NewSplit {
	n := NewSplit{
		Title: b.Title, Description: b.Description, TeamSize: b.TeamSize,
		MaxMembers: b.MaxMembers, Roles: rolesFromDTO(b.Roles), AutoFinishAt: b.AutoFinishAt,
	}
	if b.RemainderPolicy != nil {
		n.RemainderPolicy = RemainderPolicy(*b.RemainderPolicy)
	}
	return n
}

func fromUpdateDTO(b gen.SplitUpdate) Patch {
	p := Patch{
		Title: b.Title, TeamSize: b.TeamSize,
		Description:  optionalFrom(b.Description),
		MaxMembers:   optionalFrom(b.MaxMembers),
		AutoFinishAt: optionalFrom(b.AutoFinishAt),
	}
	if b.RemainderPolicy != nil {
		v := RemainderPolicy(*b.RemainderPolicy)
		p.RemainderPolicy = &v
	}
	if b.Roles != nil {
		roles := rolesFromDTO(*b.Roles)
		p.Roles = &roles
	}
	return p
}

func rolesFromDTO(in []gen.SplitRoleInput) []RoleInput {
	out := make([]RoleInput, len(in))
	for i, r := range in {
		out[i] = RoleInput{Name: r.Name, PerTeamMin: r.PerTeamMin, PerTeamMax: r.PerTeamMax, SortOrder: r.SortOrder}
	}
	return out
}

func optionalFrom[T any](n nullable.Nullable[T]) Optional[T] {
	switch {
	case !n.IsSpecified():
		return Optional[T]{}
	case n.IsNull():
		return Optional[T]{Set: true}
	default:
		v := n.MustGet()
		return Optional[T]{Set: true, Value: &v}
	}
}
