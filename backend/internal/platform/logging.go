package platform

import (
	"log/slog"
	"os"
	"strings"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
)

// NewLogger создаёт JSON-логгер; неизвестный уровень трактуется как info.
func NewLogger(level string) *slog.Logger {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(strings.ToUpper(level))); err != nil {
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
}

// RequestLogger пишет debug-запись на каждый запрос. Заголовки, cookie и тела
// не логируются: там токены и пароли.
func RequestLogger(log *slog.Logger) echo.MiddlewareFunc {
	return middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogMethod:    true,
		LogURIPath:   true,
		LogRoutePath: true,
		LogStatus:    true,
		LogLatency:   true,
		LogRemoteIP:  true,
		LogRequestID: true,
		// чтобы в статусе был итог после HTTPErrorHandler, а не 200
		HandleError: true,
		LogValuesFunc: func(_ *echo.Context, v middleware.RequestLoggerValues) error {
			attrs := []any{
				"method", v.Method, "path", v.URIPath, "route", v.RoutePath,
				"status", v.Status, "latency_ms", v.Latency.Milliseconds(),
				"remote_ip", v.RemoteIP, "request_id", v.RequestID,
			}
			if v.Error != nil {
				attrs = append(attrs, "err", v.Error)
			}
			log.Debug("request", attrs...)
			return nil
		},
	})
}
