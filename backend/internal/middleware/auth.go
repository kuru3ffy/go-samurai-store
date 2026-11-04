package middleware

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kuru3ffy/go-samurai-store/backend/internal/handler"
)

func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenString, err := c.Cookie(handler.AuthTokenCookieName)
		if err != nil {
			log.Println("認証ミドルウェア: クッキーがありません")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "認証が必要です"})
			return
		}

		claims, err := handler.VerifyToken(tokenString)
		if err != nil {
			log.Printf("認証ミドルウェア: トークンが無効です: %v", err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "認証トークンが無効です"})
			return
		}

		c.Set("user", claims)
		c.Next()
	}
}

func AdminAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		userClaims, exists := c.Get("user")
		if !exists {
			log.Println("管理者権限ミドルウェア: ユーザー情報がコンテキストに存在しません")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "認証が必要です"})
			return
		}

		claims, ok := userClaims.(*handler.JWTCustomClaims)
		if !ok {
			log.Println("管理者権限ミドルウェア: ユーザー情報の型が不正です")
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "アクセス権限がありません"})
			return
		}
		if !claims.IsAdmin {
			log.Printf("管理者権限ミドルウェア: 権限がありません (IsAdmin=%v)", claims.IsAdmin)
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "アクセス権限がありません"})
			return
		}

		c.Next()
	}
}
