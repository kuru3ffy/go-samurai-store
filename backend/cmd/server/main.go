package main

import (
	"database/sql"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/mysql"
	_ "github.com/golang-migrate/migrate/v4/source/file"

	"github.com/kuru3ffy/go-samurai-store/backend/internal/database"
	"github.com/kuru3ffy/go-samurai-store/backend/internal/handler"
	"github.com/kuru3ffy/go-samurai-store/backend/internal/middleware"
)

func main() {
	if os.Getenv("JWT_SECRET") == "" {
		log.Fatal("エラー: 環境変数JWT_SECRETが設定されていません")
	}

	runMigration()
	database.InitDB()

	router := gin.Default()
	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:3000"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Content-Type"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	router.StaticFS("/uploads", http.Dir("uploads"))

	api := router.Group("/api")
	{
		api.GET("/sample", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"status":  "ok",
				"message": "Hello, World!",
			})
		})
		api.GET("/products", handler.GetProductsHandler)
		api.GET("/products/:id", handler.GetProductByIDHandler)
		api.GET("/home", handler.GetHomePageProductsHandler)
		api.POST("/users", handler.RegisterUserHandler)
		api.POST("/inquiries", handler.CreateInquiryHandler)

		auth := api.Group("/auth")
		{
			auth.POST("/login", handler.LoginHandler)
			auth.POST("/logout", handler.LogoutHandler)
		}

		authorized := api.Group("/")
		authorized.Use(middleware.AuthMiddleware())
		{
			authorized.GET("/users/me", handler.GetUserMeHandler)
		}

		admin := api.Group("/")
		admin.Use(middleware.AuthMiddleware())
		admin.Use(middleware.AdminAuthMiddleware())
		{
			admin.POST("/products", handler.AdminCreateProductHandler)
			admin.PUT("/products/:id", handler.AdminUpdateProductHandler)
			admin.DELETE("/products/:id", handler.AdminDeleteProductHandler)
			admin.GET("/inquiries", handler.ListInquiriesHandler)
		}
	}

	log.Println("Ginサーバーをポート8080で起動します")
	if err := router.Run(":8080"); err != nil {
		log.Fatal(err)
	}
}

func runMigration() {
	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		log.Fatal("エラー: 環境変数DB_DSNが設定されていません")
	}

	log.Println("マイグレーションを開始します...")

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("データベースへの接続準備に失敗しました: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("データベースへの接続に失敗しました: %v", err)
	}

	driver, err := mysql.WithInstance(db, &mysql.Config{})
	if err != nil {
		log.Fatalf("マイグレーション用のデータベースドライバーの作成に失敗しました: %v", err)
	}

	m, err := migrate.NewWithDatabaseInstance("file://db/migrations", "mysql", driver)
	if err != nil {
		log.Fatalf("マイグレーションのセットアップに失敗しました: %v", err)
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		log.Fatalf("マイグレーションに失敗しました: %v", err)
	}

	log.Println("マイグレーションに成功しました！")
}
