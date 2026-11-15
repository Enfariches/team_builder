package distribution

import (
	"context"
	"fmt"

	"builder/backend/internal/api/gen"
)

type Handler struct {
	svc      *Service
	identity IdentityProvider
}

func NewHandler(svc *Service, identity IdentityProvider) *Handler {
	return &Handler{svc: svc, identity: identity}
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

func (h *Handler) PreviewDistribution(ctx context.Context, req gen.PreviewDistributionRequestObject) (gen.PreviewDistributionResponseObject, error) {
	uid, err := h.userID(ctx)
	if err != nil {
		return nil, fmt.Errorf("distribution.Handler.PreviewDistribution: %w", err)
	}
	// тело необязательно: без него seed случайный
	var seed *int64
	if req.Body != nil {
		seed = req.Body.Seed
	}
	v, err := h.svc.Preview(ctx, uid, req.SplitId, seed)
	if err != nil {
		return nil, fmt.Errorf("distribution.Handler.PreviewDistribution: %w", err)
	}
	return gen.PreviewDistribution200JSONResponse(runDTO(v)), nil
}

func (h *Handler) PublishDistribution(ctx context.Context, req gen.PublishDistributionRequestObject) (gen.PublishDistributionResponseObject, error) {
	uid, err := h.userID(ctx)
	if err != nil {
		return nil, fmt.Errorf("distribution.Handler.PublishDistribution: %w", err)
	}
	var runID *int64
	if req.Body != nil {
		runID = req.Body.RunId
	}
	v, err := h.svc.Publish(ctx, uid, req.SplitId, runID)
	if err != nil {
		return nil, fmt.Errorf("distribution.Handler.PublishDistribution: %w", err)
	}
	return gen.PublishDistribution200JSONResponse(runDTO(v)), nil
}

func (h *Handler) ListDistributionRuns(ctx context.Context, req gen.ListDistributionRunsRequestObject) (gen.ListDistributionRunsResponseObject, error) {
	uid, err := h.userID(ctx)
	if err != nil {
		return nil, fmt.Errorf("distribution.Handler.ListDistributionRuns: %w", err)
	}
	runs, err := h.svc.ListRuns(ctx, uid, req.SplitId)
	if err != nil {
		return nil, fmt.Errorf("distribution.Handler.ListDistributionRuns: %w", err)
	}
	out := make(gen.ListDistributionRuns200JSONResponse, len(runs))
	for i, r := range runs {
		out[i] = gen.DistributionRunSummary{
			Id: &r.ID, Algorithm: &r.Algorithm, Seed: &r.Seed, Published: ptr(r.Published()),
			Metrics: metricsDTO(r.Metrics), CreatedAt: &r.CreatedAt, PublishedAt: r.PublishedAt,
		}
	}
	return out, nil
}

func runDTO(v RunView) gen.DistributionRun {
	r := v.Run
	teams := make([]gen.Team, len(v.Teams))
	// у опубликованного прогона команды созданы в момент публикации
	created := r.CreatedAt
	if r.PublishedAt != nil {
		created = *r.PublishedAt
	}
	for i, t := range v.Teams {
		members := make([]gen.TeamMember, len(t.Members))
		for j, m := range t.Members {
			members[j] = gen.TeamMember{MemberId: ptr(m.MemberID), User: userDTO(m.User), AssignedRole: ptr(m.Role)}
		}
		teams[i] = gen.Team{Id: t.ID, SplitId: &r.SplitID, Name: ptr(t.Name), Members: &members, CreatedAt: &created}
	}
	waitlist := make([]gen.UserBrief, len(v.Waitlist))
	for i, u := range v.Waitlist {
		waitlist[i] = *userDTO(u)
	}
	return gen.DistributionRun{
		Id: &r.ID, SplitId: &r.SplitID, Algorithm: &r.Algorithm, Seed: &r.Seed,
		Published: ptr(r.Published()), Metrics: metricsDTO(r.Metrics),
		Teams: &teams, Waitlist: &waitlist, CreatedAt: &r.CreatedAt, PublishedAt: r.PublishedAt,
	}
}

func metricsDTO(m Metrics) *gen.DistributionMetrics {
	spread, share := float32(m.ExperienceSpread), float32(m.FirstChoiceRoleShare)
	return &gen.DistributionMetrics{
		TeamsCount: &m.TeamsCount, MembersAssigned: &m.MembersAssigned, MembersWaitlisted: &m.MembersWaitlisted,
		RoleQuotasSatisfied: &m.RoleQuotasSatisfied, FirstChoiceRoleShare: &share, ExperienceSpread: &spread,
	}
}

func userDTO(u UserBrief) *gen.UserBrief {
	return &gen.UserBrief{Id: &u.ID, FirstName: &u.FirstName, LastName: &u.LastName, Link: u.Link}
}

func ptr[T any](v T) *T { return &v }
