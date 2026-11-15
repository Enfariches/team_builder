package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"

	"builder/backend/internal/api/gen"
	"builder/backend/internal/auth"
	"builder/backend/internal/distribution"
	"builder/backend/internal/form"
	"builder/backend/internal/invite"
	"builder/backend/internal/member"
	"builder/backend/internal/platform"
	"builder/backend/internal/split"
	"builder/backend/internal/team"
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

	hub := split.NewHub()
	defer hub.Close()
	// SSE-потоки живут сколько угодно, поэтому закрываем их по сигналу:
	// иначе graceful shutdown будет ждать их до таймаута
	go func() {
		<-ctx.Done()
		hub.Close()
	}()

	e, err := newRouter(log, cfg, pool, hub)
	if err != nil {
		return err
	}

	sc := echo.StartConfig{Address: ":" + cfg.Port}
	log.Info("listening", "port", cfg.Port, "env", cfg.AppEnv)
	if err := sc.Start(ctx, e); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}

// newRouter собирает модули и HTTP-роутер. Отдельной функцией, чтобы сквозные
// тесты проверяли ту же сборку, что работает в проде.
func newRouter(log *slog.Logger, cfg platform.Config, pool *pgxpool.Pool, hub *split.Hub) (*echo.Echo, error) {
	userSvc := user.NewService(user.NewRepository(pool))
	tokens := auth.NewTokenIssuer(cfg.JWTSecret, cfg.AccessTTL)
	authSvc, err := auth.NewService(userStoreAdapter{userSvc}, auth.NewPgTxRunner(pool), tokens, cfg.RefreshTTL)
	if err != nil {
		return nil, fmt.Errorf("init auth: %w", err)
	}

	formSvc := form.NewService(form.NewRepository(pool))

	// split и member нужны друг другу при сборке; цикл замыкается после
	// создания split.Service, пакеты при этом друг друга не импортируют
	splitReader := &memberSplitReader{}
	inviteRedeemer := &inviteRedeemerAdapter{}
	memberSvc := member.NewService(
		member.NewRepository(pool), member.NewPgTxRunner(pool),
		inviteRedeemer, formSnapshotAdapter{formSvc}, splitReader, userDirectoryAdapter{userSvc}, hubPublisher{hub},
	)
	membership := &splitMembershipAdapter{svc: memberSvc}
	splitSvc := split.NewService(split.NewRepository(pool), split.NewPgTxRunner(pool), membership, membership, hub)
	splitReader.svc = splitSvc

	inviteSvc := invite.NewService(invite.NewRepository(pool), splitAccessAdapter{splitSvc}, splitInfoAdapter{splitSvc})
	inviteRedeemer.svc = inviteSvc

	// публикация распределения пишет в таблицы split, member, team и
	// distribution одной транзакцией; она едет в context (platform.TxManager)
	txm := platform.NewTxManager(pool)
	teamSvc := team.NewService(team.NewRepository(pool), teamSplitReader{splitSvc},
		teamMemberDirectory{memberSvc}, teamUserDirectory{userSvc})
	membership.teams = teamSvc
	distributionSvc := distribution.NewService(distribution.NewRepository(pool), txm,
		distSplitGate{splitSvc}, distRoster{memberSvc}, teamSvc, distUserDirectory{userSvc}, distEvents{hub})

	swagger, err := gen.GetSwagger()
	if err != nil {
		return nil, fmt.Errorf("load spec: %w", err)
	}

	srv := &apiServer{
		authAPI:   auth.NewHandler(authSvc, cfg.SecureCookies()),
		userAPI:   user.NewHandler(userSvc, auth.Identity{}),
		splitAPI:  split.NewHandler(splitSvc, hub, auth.Identity{}, split.DefaultPingInterval),
		formAPI:   form.NewHandler(formSvc, auth.Identity{}),
		inviteAPI: invite.NewHandler(inviteSvc, auth.Identity{}, cfg.InviteBaseURL),
		memberAPI: member.NewHandler(memberSvc, auth.Identity{}),

		distributionAPI: distribution.NewHandler(distributionSvc, auth.Identity{}),
		teamAPI:         team.NewHandler(teamSvc, auth.Identity{}),
		pool:            pool,
	}
	strict := gen.NewStrictHandler(srv, []gen.StrictMiddlewareFunc{
		auth.StrictMiddleware(tokens, publicOperations(swagger)),
	})

	e := echo.New()
	e.HTTPErrorHandler = platform.NewErrorHandler(log, slices.Concat(auth.HTTPErrors, user.HTTPErrors, split.HTTPErrors, form.HTTPErrors, invite.HTTPErrors, member.HTTPErrors, distribution.HTTPErrors, team.HTTPErrors)...)
	e.Use(middleware.RequestID(), platform.RequestLogger(log), middleware.Recover())
	if len(cfg.CORSOrigins) > 0 {
		e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
			AllowOrigins:     cfg.CORSOrigins,
			AllowCredentials: true,
		}))
	}
	gen.RegisterHandlersWithBaseURL(e, strict, apiBaseURL)

	return e, nil
}
