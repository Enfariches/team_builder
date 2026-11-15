package distribution

import (
	"net/http"

	"builder/backend/internal/platform"
)

// HTTPErrors — маппинг доменных ошибок в HTTP для platform.NewErrorHandler.
// InfeasibleError несёт details через platform.DetailsError.
var HTTPErrors = []platform.ErrorRule{
	{Target: ErrNotEnoughMembers, Status: http.StatusConflict, Code: "not_enough_members", Message: "Участников меньше, чем требуется для одной команды"},
	{Target: ErrInfeasible, Status: http.StatusConflict, Code: "distribution_infeasible", Message: "Состав нельзя разбить на команды с соблюдением квот ролей"},
	{Target: ErrRosterChanged, Status: http.StatusConflict, Code: "roster_changed", Message: "Состав участников или правила изменились после пробного прогона, сделайте новый"},
	{Target: ErrAlreadyFinished, Status: http.StatusConflict, Code: "already_finished", Message: "Распределение уже завершено"},
	{Target: ErrSplitNotOpen, Status: http.StatusConflict, Code: "split_not_open", Message: "Набор участников ещё не открыт"},
	{Target: ErrRunNotFound, Status: http.StatusNotFound, Code: "not_found", Message: "Прогон не найден"},
	{Target: ErrSplitNotFound, Status: http.StatusNotFound, Code: "not_found", Message: "Пространство не найдено"},
	{Target: ErrForbidden, Status: http.StatusForbidden, Code: "forbidden", Message: "Действие доступно только владельцу пространства"},
}
