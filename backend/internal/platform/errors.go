package platform

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v5"
)

// ErrorRule связывает доменную ошибку модуля с HTTP-ответом. Модули отдают
// свои правила наружу, platform их не импортирует.
type ErrorRule struct {
	Target  error
	Status  int
	Code    string
	Message string
}

// FieldsError реализуют ошибки валидации модулей: пары «поле, сообщение».
type FieldsError interface {
	error
	Fields() [][2]string
}

// CodedError позволяет ошибке валидации заменить общий код validation_failed
// своим (например, invalid_team_rules). Пустой code оставляет общий.
type CodedError interface {
	ErrorCode() (code, message string)
}

// Messager позволяет доменной ошибке задать динамическое сообщение, не
// меняя код и статус из ErrorRule.
type Messager interface {
	UserMessage() string
}

type errorBody struct {
	Code      string        `json:"code"`
	Message   string        `json:"message"`
	Details   []errorDetail `json:"details,omitempty"`
	RequestID string        `json:"requestId,omitempty"`
}

type errorDetail struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// NewErrorHandler собирает единый обработчик ошибок в формате Error из спеки.
func NewErrorHandler(log *slog.Logger, rules ...ErrorRule) echo.HTTPErrorHandler {
	return func(c *echo.Context, err error) {
		if resp, _ := echo.UnwrapResponse(c.Response()); resp != nil && resp.Committed {
			return
		}

		status, body := http.StatusInternalServerError, errorBody{
			Code:    "internal_error",
			Message: "Внутренняя ошибка сервера",
		}

		var fe FieldsError
		var sc echo.HTTPStatusCoder
		switch {
		case errors.As(err, &fe):
			status = http.StatusUnprocessableEntity
			body = errorBody{Code: "validation_failed", Message: "Проверьте правильность заполнения полей"}
			if ce, ok := fe.(CodedError); ok {
				if code, msg := ce.ErrorCode(); code != "" {
					body.Code, body.Message = code, msg
				}
			}
			for _, f := range fe.Fields() {
				body.Details = append(body.Details, errorDetail{Field: f[0], Message: f[1]})
			}
		case errors.As(err, &sc):
			// ошибки роутера и биндинга echo (404, 405, 400) несут свой статус
			status = sc.StatusCode()
			body = errorBody{Code: httpCode(status), Message: httpMessage(status)}
		default:
			for _, r := range rules {
				if errors.Is(err, r.Target) {
					status, body = r.Status, errorBody{Code: r.Code, Message: r.Message}
					var m Messager
					if errors.As(err, &m) {
						body.Message = m.UserMessage()
					}
					break
				}
			}
		}

		body.RequestID = c.Response().Header().Get(echo.HeaderXRequestID)
		if status >= http.StatusInternalServerError {
			log.Error("request failed", "err", err, "request_id", body.RequestID)
		}
		if err := c.JSON(status, body); err != nil {
			log.Error("write error response", "err", err)
		}
	}
}

func httpCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "bad_request"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusMethodNotAllowed:
		return "method_not_allowed"
	default:
		return "error"
	}
}

func httpMessage(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "Некорректный запрос"
	case http.StatusNotFound:
		return "Ресурс не найден"
	case http.StatusUnauthorized:
		return "Токен отсутствует, недействителен или истёк"
	case http.StatusForbidden:
		return "Недостаточно прав"
	case http.StatusMethodNotAllowed:
		return "Метод не поддерживается"
	default:
		return http.StatusText(status)
	}
}
