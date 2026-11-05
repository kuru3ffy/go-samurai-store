package handler

import (
	"database/sql"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
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

type UserUpdateRequest struct {
	Name  string `json:"name" binding:"required"`
	Email string `json:"email" binding:"required,email"`
}

type PasswordUpdateRequest struct {
	OldPassword string `json:"oldPassword" binding:"required"`
	NewPassword string `json:"newPassword" binding:"required"`
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

func UpdateUserHandler(c *gin.Context) {
	userClaims, exists := c.Get("user")
	if !exists {
		log.Println("UpdateUserHandler: ユーザー情報がコンテキストにありません")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "認証が必要です"})
		return
	}
	claims, ok := userClaims.(*JWTCustomClaims)
	if !ok {
		log.Println("UpdateUserHandler: ユーザー情報の型アサーションに失敗しました")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		return
	}

	var request UserUpdateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		log.Printf("会員編集リクエストのバインドエラー: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "入力内容が正しくありません"})
		return
	}
	request.Name = strings.TrimSpace(request.Name)
	request.Email = strings.TrimSpace(request.Email)
	if request.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "氏名を入力してください"})
		return
	}

	emailPattern := regexp.MustCompile(`^[a-zA-Z0-9.]+@[a-zA-Z0-9.]+$`)
	if !emailPattern.MatchString(request.Email) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "正しいメールアドレスの形式で入力してください"})
		return
	}

	db := database.GetDB()
	var count int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM users WHERE email = ? AND id != ?",
		request.Email,
		claims.UserID,
	).Scan(&count); err != nil {
		log.Printf("メールアドレス重複チェックエラー（更新時）: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		return
	}
	if count > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "このメールアドレスは既に使用されています"})
		return
	}

	now := time.Now()
	newClaims := &JWTCustomClaims{
		UserID:  claims.UserID,
		Name:    request.Name,
		Email:   request.Email,
		IsAdmin: claims.IsAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}
	newTokenString, err := jwt.NewWithClaims(jwt.SigningMethodHS256, newClaims).SignedString(JWTSecret)
	if err != nil {
		log.Printf("JWT再署名エラー（更新時）: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		return
	}

	result, err := db.Exec(
		"UPDATE users SET name = ?, email = ? WHERE id = ? AND enabled = TRUE",
		request.Name,
		request.Email,
		claims.UserID,
	)
	if err != nil {
		log.Printf("会員情報更新エラー (UserID=%d): %v", claims.UserID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		return
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		log.Printf("会員情報更新件数取得エラー (UserID=%d): %v", claims.UserID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		return
	}
	if rowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "ユーザーが見つかりません"})
		return
	}

	c.SetCookie(AuthTokenCookieName, newTokenString, 3600, "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{"message": "会員情報を編集しました"})
}

func UpdatePasswordHandler(c *gin.Context) {
	userClaims, exists := c.Get("user")
	if !exists {
		log.Println("UpdatePasswordHandler: ユーザー情報がコンテキストにありません")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "認証が必要です"})
		return
	}
	claims, ok := userClaims.(*JWTCustomClaims)
	if !ok {
		log.Println("UpdatePasswordHandler: ユーザー情報の型アサーションに失敗しました")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		return
	}

	var request PasswordUpdateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		log.Printf("パスワード変更リクエストのバインドエラー: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "入力内容が正しくありません"})
		return
	}
	if len(request.NewPassword) < 8 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "パスワードは8文字以上で入力してください"})
		return
	}
	if len([]byte(request.NewPassword)) > 72 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "パスワードは72バイト以内で入力してください"})
		return
	}

	db := database.GetDB()
	var currentPasswordHash string
	err := db.QueryRow(
		"SELECT password FROM users WHERE id = ? AND enabled = TRUE",
		claims.UserID,
	).Scan(&currentPasswordHash)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Printf("パスワード変更エラー: ユーザーが見つかりません (UserID=%d)", claims.UserID)
			c.JSON(http.StatusNotFound, gin.H{"error": "ユーザー情報が見つかりません"})
		} else {
			log.Printf("現在のパスワード取得エラー (UserID=%d): %v", claims.UserID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		}
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(currentPasswordHash), []byte(request.OldPassword)); err != nil {
		log.Printf("パスワード変更失敗（現在のパスワード不一致）: UserID=%d", claims.UserID)
		c.JSON(http.StatusBadRequest, gin.H{"error": "現在のパスワードが正しくありません"})
		return
	}

	newPasswordHash, err := bcrypt.GenerateFromPassword([]byte(request.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("新しいパスワードのハッシュ化エラー (UserID=%d): %v", claims.UserID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		return
	}
	result, err := db.Exec(
		"UPDATE users SET password = ? WHERE id = ? AND enabled = TRUE",
		string(newPasswordHash),
		claims.UserID,
	)
	if err != nil {
		log.Printf("パスワード更新エラー (UserID=%d): %v", claims.UserID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		return
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		log.Printf("パスワード更新件数取得エラー (UserID=%d): %v", claims.UserID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		return
	}
	if rowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "ユーザー情報が見つかりません"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "パスワードを変更しました"})
}
