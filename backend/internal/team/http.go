package team

import (
	"net/http"

	"builder/backend/internal/platform"
)

// HTTPErrors — маппинг доменных ошибок в HTTP для platform.NewErrorHandler.
var HTTPErrors = []platform.ErrorRule{
	{Target: ErrNotFinished, Status: http.StatusConflict, Code: "split_not_finished", Message: "Распределение ещё не опубликовано"},
	{Target: ErrNotAssigned, Status: http.StatusNotFound, Code: "not_assigned", Message: "Вы не распределены в команду"},
	{Target: ErrSplitNotFound, Status: http.StatusNotFound, Code: "not_found", Message: "Пространство не найдено"},
	{Target: ErrForbidden, Status: http.StatusForbidden, Code: "forbidden", Message: "Действие доступно только владельцу пространства"},
}
