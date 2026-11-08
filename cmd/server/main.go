package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/example/pns/internal/config"
	"github.com/example/pns/internal/db"
	"github.com/example/pns/internal/repository"
	"github.com/example/pns/internal/server"
	"github.com/example/pns/internal/service"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("connect db: %v", err)
	}
	defer pool.Close()

	if err := db.Migrate(ctx, pool); err != nil {
		log.Fatalf("migrate db: %v", err)
	}

	userRepo := repository.NewUserRepository(pool)
	deviceRepo := repository.NewDeviceRepository(pool)
	promoRepo := repository.NewPromotionRepository(pool)

	authSvc := service.NewAuthService(userRepo)
	service.Bootstrap(ctx, userRepo, authSvc, os.Getenv("ADMIN_EMAIL"), os.Getenv("ADMIN_PASSWORD"))

	srv := server.New(cfg, ctx, struct {
		Users   *repository.UserRepository
		Devices *repository.DeviceRepository
		Promos  *repository.PromotionRepository
	}{
		Users:   userRepo,
		Devices: deviceRepo,
		Promos:  promoRepo,
	})

	if err := srv.Run(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

