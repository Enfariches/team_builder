package invite

import (
	"net/http"

	"builder/backend/internal/platform"
)

// HTTPErrors — маппинг доменных ошибок в HTTP для platform.NewErrorHandler.
var HTTPErrors = []platform.ErrorRule{
	{Target: ErrInviteInvalid, Status: http.StatusNotFound, Code: "invite_not_found", Message: "Приглашение не найдено, отозвано или истекло"},
	{Target: ErrNotFound, Status: http.StatusNotFound, Code: "not_found", Message: "Приглашение не найдено"},
	{Target: ErrSplitNotFound, Status: http.StatusNotFound, Code: "not_found", Message: "Пространство не найдено"},
	{Target: ErrForbidden, Status: http.StatusForbidden, Code: "forbidden", Message: "Действие доступно только владельцу пространства"},
}
