package handler

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/kuru3ffy/go-samurai-store/backend/internal/database"
)

type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type userForAuth struct {
	ID       int
	Name     string
	Email    string
	Password string
	IsAdmin  bool
}

type JWTCustomClaims struct {
	UserID  int    `json:"userId"`
	Name    string `json:"name"`
	Email   string `json:"email"`
	IsAdmin bool   `json:"isAdmin"`
	jwt.RegisteredClaims
}

var JWTSecret = []byte(os.Getenv("JWT_SECRET"))

var Domain = os.Getenv("COOKIE_DOMAIN")
var IsSecure = os.Getenv("APP_ENV") == "production"

const AuthTokenCookieName = "authToken"

func VerifyToken(tokenString string) (*JWTCustomClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &JWTCustomClaims{}, func(token *jwt.Token) (any, error) {
		if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, errors.New("予期しない署名方法です")
		}
		return JWTSecret, nil
	})
	if err != nil {
		log.Printf("トークン検証エラー: %v", err)
		return nil, err
	}

	claims, ok := token.Claims.(*JWTCustomClaims)
	if !ok || !token.Valid {
		return nil, errors.New("無効なトークンです")
	}
	return claims, nil
}

func LoginHandler(c *gin.Context) {
	var request LoginRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		log.Printf("ログインリクエストのバインドエラー: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "メールアドレスまたはパスワードが正しくありません"})
		return
	}

	db := database.GetDB()
	var user userForAuth
	err := db.QueryRow(`
		SELECT id, name, email, password, is_admin
		FROM users
		WHERE email = ? AND enabled = TRUE
	`, request.Email).Scan(
		&user.ID,
		&user.Name,
		&user.Email,
		&user.Password,
		&user.IsAdmin,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Printf("ログイン失敗（ユーザーが見つからないか無効）: email=%s", request.Email)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "メールアドレスまたはパスワードが違います"})
		} else {
			log.Printf("ユーザー検索エラー: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		}
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(request.Password)); err != nil {
		log.Printf("ログイン失敗（パスワード不一致）: email=%s", request.Email)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "メールアドレスまたはパスワードが違います"})
		return
	}

	now := time.Now()
	claims := &JWTCustomClaims{
		UserID:  user.ID,
		Name:    user.Name,
		Email:   user.Email,
		IsAdmin: user.IsAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}
	tokenString, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(JWTSecret)
	if err != nil {
		log.Printf("JWT署名エラー: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		return
	}

	c.SetCookie(AuthTokenCookieName, tokenString, 3600, "/", Domain, IsSecure, true)
	c.JSON(http.StatusOK, gin.H{
		"message": "ログイン成功",
		"isAdmin": user.IsAdmin,
	})
}

func LogoutHandler(c *gin.Context) {
	c.SetCookie(AuthTokenCookieName, "", -1, "/", Domain, IsSecure, true)
	c.Redirect(http.StatusFound, "/?logged-out=1")
}
