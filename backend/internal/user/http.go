package user

import (
	"net/http"

	"builder/backend/internal/platform"
)

// HTTPErrors — маппинг доменных ошибок в HTTP для platform.NewErrorHandler.
var HTTPErrors = []platform.ErrorRule{
	{Target: ErrNotFound, Status: http.StatusNotFound, Code: "not_found", Message: "Пользователь не найден"},
}
