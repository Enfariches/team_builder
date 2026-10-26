package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"

	"builder/backend/internal/api/gen"
	"builder/backend/internal/auth"
	"builder/backend/internal/platform"
	"builder/backend/internal/split"
	"builder/backend/internal/user"
)

const apiBaseURL = "/api/v1"

func main() {
	cfg, err := platform.LoadConfig()
	if err != nil {
		slog.New(slog.NewJSONHandler(os.Stdout, nil)).Error("fatal", "err", err)
		os.Exit(1)
	}
	log := platform.NewLogger(cfg.LogLevel)
	if err := run(log, cfg); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger, cfg platform.Config) error {

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect db: %w", err)
	}
	defer pool.Close()

	userSvc := user.NewService(user.NewRepository(pool))
	tokens := auth.NewTokenIssuer(cfg.JWTSecret, cfg.AccessTTL)
	authSvc, err := auth.NewService(userStoreAdapter{userSvc}, auth.NewPgTxRunner(pool), tokens, cfg.RefreshTTL)
	if err != nil {
		return fmt.Errorf("init auth: %w", err)
	}

	hub := split.NewHub()
	defer hub.Close()
	// SSE-потоки живут сколько угодно, поэтому закрываем их по сигналу:
	// иначе graceful shutdown будет ждать их до таймаута
	go func() {
		<-ctx.Done()
		hub.Close()
	}()
	splitSvc := split.NewService(split.NewRepository(pool), split.NewPgTxRunner(pool), memberStub{}, memberStub{}, hub)

	swagger, err := gen.GetSwagger()
	if err != nil {
		return fmt.Errorf("load spec: %w", err)
	}

	srv := &apiServer{
		authAPI:  auth.NewHandler(authSvc, cfg.SecureCookies()),
		userAPI:  user.NewHandler(userSvc, auth.Identity{}),
		splitAPI: split.NewHandler(splitSvc, hub, auth.Identity{}, split.DefaultPingInterval),
		pool:     pool,
	}
	strict := gen.NewStrictHandler(srv, []gen.StrictMiddlewareFunc{
		auth.StrictMiddleware(tokens, publicOperations(swagger)),
	})

	e := echo.New()
	e.HTTPErrorHandler = platform.NewErrorHandler(log, append(append(auth.HTTPErrors, user.HTTPErrors...), split.HTTPErrors...)...)
	e.Use(middleware.RequestID(), platform.RequestLogger(log), middleware.Recover())
	if len(cfg.CORSOrigins) > 0 {
		e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
			AllowOrigins:     cfg.CORSOrigins,
			AllowCredentials: true,
		}))
	}
	gen.RegisterHandlersWithBaseURL(e, strict, apiBaseURL)

	sc := echo.StartConfig{Address: ":" + cfg.Port}
	log.Info("listening", "port", cfg.Port, "env", cfg.AppEnv)
	if err := sc.Start(ctx, e); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}
