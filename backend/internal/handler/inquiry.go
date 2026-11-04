package handler

import (
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kuru3ffy/go-samurai-store/backend/internal/database"
)

type InquiryCreateRequest struct {
	Name    string `json:"name" binding:"required"`
	Email   string `json:"email" binding:"required,email"`
	Message string `json:"message" binding:"required"`
}

type InquiryResponse struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

func CreateInquiryHandler(c *gin.Context) {
	var request InquiryCreateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		log.Printf("お問い合わせ入力エラー: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "入力内容が正しくありません"})
		return
	}

	_, err := database.GetDB().Exec(
		"INSERT INTO inquiries (name, email, message) VALUES (?, ?, ?)",
		request.Name, request.Email, request.Message,
	)
	if err != nil {
		log.Printf("お問い合わせ登録エラー: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "お問い合わせを送信しました"})
}

func ListInquiriesHandler(c *gin.Context) {
	rows, err := database.GetDB().Query(`
		SELECT id, name, email, message, created_at
		FROM inquiries
		ORDER BY created_at DESC, id DESC
	`)
	if err != nil {
		log.Printf("お問い合わせ一覧取得エラー: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		return
	}
	defer rows.Close()

	inquiries := make([]InquiryResponse, 0)
	for rows.Next() {
		var inquiry InquiryResponse
		if err := rows.Scan(
			&inquiry.ID,
			&inquiry.Name,
			&inquiry.Email,
			&inquiry.Message,
			&inquiry.CreatedAt,
		); err != nil {
			log.Printf("お問い合わせデータ読み取りエラー: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
			return
		}
		inquiries = append(inquiries, inquiry)
	}
	if err := rows.Err(); err != nil {
		log.Printf("お問い合わせ一覧取得中のエラー: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		return
	}

	c.JSON(http.StatusOK, inquiries)
}
