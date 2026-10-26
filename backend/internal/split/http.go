package split

import (
	"net/http"

	"builder/backend/internal/platform"
)

// HTTPErrors — маппинг доменных ошибок в HTTP для platform.NewErrorHandler.
var HTTPErrors = []platform.ErrorRule{
	{Target: ErrNotFound, Status: http.StatusNotFound, Code: "not_found", Message: "Пространство не найдено"},
	{Target: ErrForbidden, Status: http.StatusForbidden, Code: "forbidden", Message: "Действие доступно только владельцу пространства"},
	{Target: ErrInvalidStatusTransition, Status: http.StatusConflict, Code: "invalid_status_transition", Message: "Недопустимый переход статуса"},
	{Target: ErrRulesLocked, Status: http.StatusConflict, Code: "invalid_status_for_update", Message: "Правила состава нельзя менять после открытия пространства"},
	{Target: ErrSplitFinished, Status: http.StatusConflict, Code: "invalid_status_for_update", Message: "Пространство завершено, изменения недопустимы"},
}
