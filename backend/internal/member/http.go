package member

import (
	"net/http"

	"builder/backend/internal/platform"
)

// HTTPErrors — маппинг доменных ошибок в HTTP для platform.NewErrorHandler.
var HTTPErrors = []platform.ErrorRule{
	{Target: ErrAlreadyMember, Status: http.StatusConflict, Code: "already_member", Message: "Вы уже участвуете в этом распределении"},
	{Target: ErrSplitFull, Status: http.StatusConflict, Code: "split_full", Message: "Свободных мест больше нет"},
	{Target: ErrSplitNotOpen, Status: http.StatusConflict, Code: "split_not_open", Message: "Набор участников закрыт"},
	{Target: ErrSplitFinished, Status: http.StatusConflict, Code: "split_finished", Message: "Распределение уже завершено"},
	{Target: ErrInviteInvalid, Status: http.StatusNotFound, Code: "invite_not_found", Message: "Приглашение не найдено, отозвано или истекло"},
	{Target: ErrFormNotFound, Status: http.StatusNotFound, Code: "form_not_found", Message: "Анкета не найдена"},
	{Target: ErrNotFound, Status: http.StatusNotFound, Code: "not_found", Message: "Участник не найден"},
	{Target: ErrSplitNotFound, Status: http.StatusNotFound, Code: "not_found", Message: "Пространство не найдено"},
	{Target: ErrForbidden, Status: http.StatusForbidden, Code: "forbidden", Message: "Действие доступно только владельцу пространства"},
}
