package handler

import (
	"log"
	"net/http"
	"regexp"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/kuru3ffy/go-samurai-store/backend/internal/database"
)

type UserRegisterRequest struct {
	Name     string `json:"name" binding:"required"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type UserMeResponse struct {
	UserID  int    `json:"userId"`
	Name    string `json:"name"`
	Email   string `json:"email"`
	IsAdmin bool   `json:"isAdmin"`
}

func RegisterUserHandler(c *gin.Context) {
	var request UserRegisterRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		log.Printf("HTTPリクエストボディのバインドエラー: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "入力内容が正しくありません"})
		return
	}

	emailPattern := regexp.MustCompile(`^[a-zA-Z0-9.]+@[a-zA-Z0-9.]+$`)
	if !emailPattern.MatchString(request.Email) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "正しいメールアドレスの形式で入力してください"})
		return
	}

	if len(request.Password) < 8 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "パスワードは8文字以上で入力してください"})
		return
	}

	db := database.GetDB()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM users WHERE email = ?", request.Email).Scan(&count); err != nil {
		log.Printf("メールアドレス重複チェックエラー: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		return
	}
	if count > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "このメールアドレスは既に登録されています"})
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(request.Password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("パスワードハッシュ化エラー: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		return
	}

	_, err = db.Exec(`
		INSERT INTO users (name, email, password, is_admin, enabled)
		VALUES (?, ?, ?, false, true)
	`, request.Name, request.Email, string(hashedPassword))
	if err != nil {
		log.Printf("ユーザー登録エラー: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "会員登録が完了しました"})
}

func GetUserMeHandler(c *gin.Context) {
	userClaims, exists := c.Get("user")
	if !exists {
		log.Println("GetUserMeHandler: コンテキストにユーザー情報が見つかりません")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "認証情報が見つかりません"})
		return
	}

	claims, ok := userClaims.(*JWTCustomClaims)
	if !ok {
		log.Println("GetUserMeHandler: ユーザー情報の型アサーションに失敗しました")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		return
	}

	c.JSON(http.StatusOK, UserMeResponse{
		UserID:  claims.UserID,
		Name:    claims.Name,
		Email:   claims.Email,
		IsAdmin: claims.IsAdmin,
	})
}
