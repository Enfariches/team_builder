package form

import (
	"context"
	"fmt"

	"github.com/oapi-codegen/nullable"

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

func (h *Handler) ListForms(ctx context.Context, _ gen.ListFormsRequestObject) (gen.ListFormsResponseObject, error) {
	uid, err := h.userID(ctx)
	if err != nil {
		return nil, fmt.Errorf("form.Handler.ListForms: %w", err)
	}
	fs, err := h.svc.List(ctx, uid)
	if err != nil {
		return nil, fmt.Errorf("form.Handler.ListForms: %w", err)
	}
	out := make(gen.ListForms200JSONResponse, len(fs))
	for i, f := range fs {
		out[i] = toDTO(f)
	}
	return out, nil
}

func (h *Handler) CreateForm(ctx context.Context, req gen.CreateFormRequestObject) (gen.CreateFormResponseObject, error) {
	uid, err := h.userID(ctx)
	if err != nil {
		return nil, fmt.Errorf("form.Handler.CreateForm: %w", err)
	}
	f, err := h.svc.Create(ctx, uid, fromCreateDTO(*req.Body))
	if err != nil {
		return nil, fmt.Errorf("form.Handler.CreateForm: %w", err)
	}
	return gen.CreateForm201JSONResponse(toDTO(f)), nil
}

func (h *Handler) GetForm(ctx context.Context, req gen.GetFormRequestObject) (gen.GetFormResponseObject, error) {
	uid, err := h.userID(ctx)
	if err != nil {
		return nil, fmt.Errorf("form.Handler.GetForm: %w", err)
	}
	f, err := h.svc.Get(ctx, uid, req.FormId)
	if err != nil {
		return nil, fmt.Errorf("form.Handler.GetForm: %w", err)
	}
	return gen.GetForm200JSONResponse(toDTO(f)), nil
}

func (h *Handler) UpdateForm(ctx context.Context, req gen.UpdateFormRequestObject) (gen.UpdateFormResponseObject, error) {
	uid, err := h.userID(ctx)
	if err != nil {
		return nil, fmt.Errorf("form.Handler.UpdateForm: %w", err)
	}
	f, err := h.svc.Update(ctx, uid, req.FormId, fromUpdateDTO(*req.Body))
	if err != nil {
		return nil, fmt.Errorf("form.Handler.UpdateForm: %w", err)
	}
	return gen.UpdateForm200JSONResponse(toDTO(f)), nil
}

func (h *Handler) DeleteForm(ctx context.Context, req gen.DeleteFormRequestObject) (gen.DeleteFormResponseObject, error) {
	uid, err := h.userID(ctx)
	if err != nil {
		return nil, fmt.Errorf("form.Handler.DeleteForm: %w", err)
	}
	if err := h.svc.Delete(ctx, uid, req.FormId); err != nil {
		return nil, fmt.Errorf("form.Handler.DeleteForm: %w", err)
	}
	return gen.DeleteForm204Response{}, nil
}

func fromCreateDTO(b gen.FormCreate) NewForm {
	n := NewForm{Name: b.Name, Comment: b.Comment}
	if b.Experience != nil {
		n.Experience = *b.Experience
	}
	if b.Skills != nil {
		n.Skills = *b.Skills
	}
	if b.RolePreferences != nil {
		n.RolePreferences = fromPrefsDTO(*b.RolePreferences)
	}
	return n
}

func fromUpdateDTO(b gen.FormUpdate) Update {
	u := Update{Name: b.Name, Experience: b.Experience, Comment: optionalFrom(b.Comment)}
	if b.Skills != nil {
		u.Skills = b.Skills
	}
	if b.RolePreferences != nil {
		p := fromPrefsDTO(*b.RolePreferences)
		u.RolePreferences = &p
	}
	return u
}

func fromPrefsDTO(in []gen.RolePreference) []RolePreference {
	out := make([]RolePreference, len(in))
	for i, p := range in {
		out[i] = RolePreference{RoleName: p.RoleName, Rank: p.Rank}
	}
	return out
}

func optionalFrom(n nullable.Nullable[string]) Optional[string] {
	switch {
	case !n.IsSpecified():
		return Optional[string]{}
	case n.IsNull():
		return Optional[string]{Set: true}
	default:
		v := n.MustGet()
		return Optional[string]{Set: true, Value: &v}
	}
}

func toDTO(f Form) gen.Form {
	prefs := make([]gen.RolePreference, len(f.RolePreferences))
	for i, p := range f.RolePreferences {
		prefs[i] = gen.RolePreference{RoleName: p.RoleName, Rank: p.Rank}
	}
	return gen.Form{
		Id:              f.ID,
		Name:            f.Name,
		Experience:      f.Experience,
		Skills:          f.Skills,
		RolePreferences: prefs,
		Comment:         f.Comment,
		CreatedAt:       f.CreatedAt,
		UpdatedAt:       f.UpdatedAt,
	}
}
