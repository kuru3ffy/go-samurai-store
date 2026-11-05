package handler

import (
	"database/sql"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kuru3ffy/go-samurai-store/backend/internal/database"
)

type ReviewData struct {
	ID        int       `json:"id"`
	ProductID int       `json:"product_id"`
	UserID    int       `json:"user_id"`
	Score     int       `json:"score"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
	UserName  string    `json:"user_name"`
}

type ReviewsPageData struct {
	Reviews    []ReviewData `json:"reviews"`
	ReviewAvg  float64      `json:"review_avg"`
	Pagination Pagination   `json:"pagination"`
}

type ReviewRequest struct {
	Rating  int    `json:"rating" binding:"required,min=1,max=5"`
	Content string `json:"content" binding:"required"`
}

func GetReviewsHandler(c *gin.Context) {
	productID, err := strconv.Atoi(c.Param("id"))
	if err != nil || productID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "商品IDが不正です"})
		return
	}
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 {
		page = 1
	}
	perPage, err := strconv.Atoi(c.DefaultQuery("perPage", "10"))
	if err != nil || perPage < 1 {
		perPage = 10
	}
	if perPage > 100 {
		perPage = 100
	}
	offset := (page - 1) * perPage
	db := database.GetDB()

	var productExists bool
	if err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM products WHERE id = ?)", productID).Scan(&productExists); err != nil {
		log.Printf("レビュー対象商品確認エラー (ProductID=%d): %v", productID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		return
	}
	if !productExists {
		c.JSON(http.StatusNotFound, gin.H{"error": "商品が見つかりませんでした"})
		return
	}

	reviews := make([]ReviewData, 0)
	var totalItems int
	var reviewAvg sql.NullFloat64
	var reviewsErr, statsErr error
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		rows, err := db.Query(`
			SELECT r.id, r.product_id, r.user_id, r.score, r.content, r.created_at, u.name
			FROM reviews AS r
			JOIN users AS u ON r.user_id = u.id
			WHERE r.product_id = ?
			ORDER BY r.created_at DESC, r.id DESC
			LIMIT ? OFFSET ?
		`, productID, perPage, offset)
		if err != nil {
			reviewsErr = err
			return
		}
		defer rows.Close()
		for rows.Next() {
			var review ReviewData
			if err := rows.Scan(
				&review.ID,
				&review.ProductID,
				&review.UserID,
				&review.Score,
				&review.Content,
				&review.CreatedAt,
				&review.UserName,
			); err != nil {
				reviewsErr = err
				return
			}
			reviews = append(reviews, review)
		}
		reviewsErr = rows.Err()
	}()

	go func() {
		defer wg.Done()
		statsErr = db.QueryRow(`
			SELECT COUNT(*), AVG(score)
			FROM reviews
			WHERE product_id = ?
		`, productID).Scan(&totalItems, &reviewAvg)
	}()

	wg.Wait()
	if reviewsErr != nil || statsErr != nil {
		log.Printf("レビュー取得エラー (ProductID=%d): list=%v stats=%v", productID, reviewsErr, statsErr)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		return
	}

	average := 0.0
	if reviewAvg.Valid {
		average = math.Round(reviewAvg.Float64*10) / 10
	}
	c.JSON(http.StatusOK, ReviewsPageData{
		Reviews:   reviews,
		ReviewAvg: average,
		Pagination: Pagination{
			CurrentPage: page,
			PerPage:     perPage,
			TotalItems:  totalItems,
			TotalPages:  int(math.Ceil(float64(totalItems) / float64(perPage))),
		},
	})
}

func CreateReviewHandler(c *gin.Context) {
	productID, err := strconv.Atoi(c.Param("id"))
	if err != nil || productID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "商品IDが不正です"})
		return
	}
	userClaims, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "レビューの投稿にはログインが必要です"})
		return
	}
	claims, ok := userClaims.(*JWTCustomClaims)
	if !ok {
		log.Println("CreateReviewHandler: ユーザー情報の型アサーションに失敗しました")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		return
	}

	var request ReviewRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "入力内容が正しくありません（評価は1〜5、コメント必須）"})
		return
	}
	request.Content = strings.TrimSpace(request.Content)
	if request.Content == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "コメントを入力してください"})
		return
	}

	db := database.GetDB()
	var productExists bool
	if err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM products WHERE id = ?)", productID).Scan(&productExists); err != nil {
		log.Printf("レビュー対象商品確認エラー (ProductID=%d): %v", productID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		return
	}
	if !productExists {
		c.JSON(http.StatusNotFound, gin.H{"error": "商品が見つかりませんでした"})
		return
	}
	if _, err := db.Exec(
		"INSERT INTO reviews (product_id, user_id, score, content) VALUES (?, ?, ?, ?)",
		productID,
		claims.UserID,
		request.Rating,
		request.Content,
	); err != nil {
		log.Printf("レビュー登録エラー: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "レビューの投稿に失敗しました"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "レビューを登録しました"})
}
