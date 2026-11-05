package handler

import (
	"database/sql"
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/kuru3ffy/go-samurai-store/backend/internal/database"
)

type FavoriteRequest struct {
	ProductID int `json:"productId" binding:"required,min=1"`
}

func favoriteUserID(c *gin.Context) (int, bool) {
	userClaims, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "認証が必要です"})
		return 0, false
	}
	claims, ok := userClaims.(*JWTCustomClaims)
	if !ok {
		log.Println("お気に入りAPI: ユーザー情報の型アサーションに失敗しました")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		return 0, false
	}
	return claims.UserID, true
}

func favoriteProductID(c *gin.Context) (int, bool) {
	productID, err := strconv.Atoi(c.Param("productId"))
	if err != nil || productID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "商品IDが不正です"})
		return 0, false
	}
	return productID, true
}

func ListFavoritesHandler(c *gin.Context) {
	userID, ok := favoriteUserID(c)
	if !ok {
		return
	}
	rows, err := database.GetDB().Query(`
		SELECT
			p.id, p.name, p.price, p.stock, p.image_url, p.updated_at,
			COALESCE(ROUND(AVG(r.score), 1), 0.0),
			COUNT(r.id)
		FROM favorites AS f
		JOIN products AS p ON f.product_id = p.id
		LEFT JOIN reviews AS r ON p.id = r.product_id
		WHERE f.user_id = ?
		GROUP BY f.id, f.created_at, p.id, p.name, p.price, p.stock, p.image_url, p.updated_at
		ORDER BY f.created_at DESC, f.id DESC
	`, userID)
	if err != nil {
		log.Printf("お気に入り一覧取得エラー (UserID=%d): %v", userID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		return
	}
	defer rows.Close()

	favorites := make([]ProductListItem, 0)
	for rows.Next() {
		var product ProductListItem
		var imageURL sql.NullString
		if err := rows.Scan(
			&product.ID,
			&product.Name,
			&product.Price,
			&product.Stock,
			&imageURL,
			&product.UpdatedAt,
			&product.ReviewAvg,
			&product.ReviewCount,
		); err != nil {
			log.Printf("お気に入りデータ読み取りエラー (UserID=%d): %v", userID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
			return
		}
		if imageURL.Valid {
			product.ImageURL = &imageURL.String
		}
		favorites = append(favorites, product)
	}
	if err := rows.Err(); err != nil {
		log.Printf("お気に入り一覧取得中のエラー (UserID=%d): %v", userID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		return
	}
	c.JSON(http.StatusOK, favorites)
}

func AddFavoriteHandler(c *gin.Context) {
	userID, ok := favoriteUserID(c)
	if !ok {
		return
	}
	var request FavoriteRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "商品IDが不正です"})
		return
	}

	db := database.GetDB()
	var productExists bool
	if err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM products WHERE id = ?)", request.ProductID).Scan(&productExists); err != nil {
		log.Printf("お気に入り対象商品確認エラー: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		return
	}
	if !productExists {
		c.JSON(http.StatusNotFound, gin.H{"error": "商品が見つかりませんでした"})
		return
	}
	if _, err := db.Exec(
		`INSERT INTO favorites (product_id, user_id)
		 VALUES (?, ?)
		 ON DUPLICATE KEY UPDATE id = id`,
		request.ProductID,
		userID,
	); err != nil {
		log.Printf("お気に入り登録エラー (UserID=%d, ProductID=%d): %v", userID, request.ProductID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "お気に入りの登録に失敗しました"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "お気に入りに追加しました"})
}

func GetFavoriteStatusHandler(c *gin.Context) {
	userID, ok := favoriteUserID(c)
	if !ok {
		return
	}
	productID, ok := favoriteProductID(c)
	if !ok {
		return
	}
	var count int
	if err := database.GetDB().QueryRow(
		"SELECT COUNT(*) FROM favorites WHERE user_id = ? AND product_id = ?",
		userID,
		productID,
	).Scan(&count); err != nil {
		log.Printf("お気に入り状態取得エラー (UserID=%d, ProductID=%d): %v", userID, productID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"isFavorite": count > 0})
}

func RemoveFavoriteHandler(c *gin.Context) {
	userID, ok := favoriteUserID(c)
	if !ok {
		return
	}
	productID, ok := favoriteProductID(c)
	if !ok {
		return
	}
	if _, err := database.GetDB().Exec(
		"DELETE FROM favorites WHERE user_id = ? AND product_id = ?",
		userID,
		productID,
	); err != nil {
		log.Printf("お気に入り削除エラー (UserID=%d, ProductID=%d): %v", userID, productID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "お気に入りの削除に失敗しました"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "お気に入りから削除しました"})
}
