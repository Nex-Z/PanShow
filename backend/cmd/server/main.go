package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"panshow/backend/internal/config"
	"panshow/backend/internal/httpapi"
	"panshow/backend/internal/model"
	"panshow/backend/internal/session"
	"panshow/backend/internal/storage"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	ctx := context.Background()
	cfg := config.Load()

	logCloser, err := setupLogging(cfg)
	if err != nil {
		log.Fatalf("setup logging: %v", err)
	}
	if logCloser != nil {
		defer logCloser.Close()
	}

	db, err := gorm.Open(postgres.Open(cfg.DatabaseURL), &gorm.Config{})
	if err != nil {
		log.Fatalf("connect database: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.DirectoryPassword{}, &model.Announcement{}, &model.SiteConfig{}, &model.FileIndexEntry{}, &model.FileIndexDir{}); err != nil {
		log.Fatalf("migrate database: %v", err)
	}
	if err := ensureInitialAdmin(db, cfg); err != nil {
		log.Fatalf("ensure initial admin: %v", err)
	}

	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Fatalf("connect redis: %v", err)
	}

	r2, err := storage.NewR2Client(ctx, cfg)
	if err != nil {
		log.Fatalf("create r2 client: %v", err)
	}

	gin.SetMode(cfg.GinMode)
	router := httpapi.NewRouter(httpapi.RouterDeps{
		Config:  cfg,
		DB:      db,
		Session: session.NewRedisStore(redisClient, cfg.SessionTTL),
		Storage: r2,
	})

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("PanShow API listening on %s", cfg.HTTPAddr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server stopped: %v", err)
	}
}

func ensureInitialAdmin(db *gorm.DB, cfg config.Config) error {
	if cfg.AdminUsername == "" || cfg.AdminPassword == "" {
		return nil
	}

	var count int64
	if err := db.Model(&model.User{}).Where("role = ?", model.RoleAdmin).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(cfg.AdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return db.Create(&model.User{
		Username:     cfg.AdminUsername,
		PasswordHash: string(hash),
		Role:         model.RoleAdmin,
		Active:       true,
	}).Error
}
