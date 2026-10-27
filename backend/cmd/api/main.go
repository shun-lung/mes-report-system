package main

import (
	"context"
	"log"
	_ "time/tzdata" // 內嵌時區資料，部署環境不需另裝

	"github.com/gin-gonic/gin"

	"mes/internal/auth"
	"mes/internal/config"
	"mes/internal/dashboard"
	"mes/internal/database"
	"mes/internal/inventory"
	"mes/internal/org"
	"mes/internal/production"
)

func main() {
	cfg := config.Load()
	ctx := context.Background()

	if err := database.Migrate(cfg.DatabaseURL); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("connect db: %v", err)
	}
	defer pool.Close()
	if err := auth.Seed(ctx, pool, cfg.AdminEmail, cfg.AdminPassword); err != nil {
		log.Fatalf("seed: %v", err)
	}

	tokens := auth.NewTokenIssuer(cfg.JWTSecret, cfg.JWTTTL)
	authStore := auth.NewStore(pool)

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	api := r.Group("/api")
	secured := api.Group("", auth.Authenticate(tokens))
	scoped := secured.Group("", auth.FactoryScope(authStore))

	authHandler := auth.NewHandler(authStore, tokens)
	authHandler.Register(api, secured)
	authHandler.RegisterAdmin(scoped)
	org.NewHandler(pool).Register(secured, scoped)
	production.NewHandler(production.NewService(pool)).Register(scoped)
	inventory.NewHandler(pool).Register(scoped)
	dashboard.NewHandler(pool).Register(scoped)

	log.Printf("listening on %s", cfg.HTTPAddr)
	if err := r.Run(cfg.HTTPAddr); err != nil {
		log.Fatal(err)
	}
}
