package handler

import (
	"database/sql"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kuru3ffy/go-samurai-store/backend/internal/database"
)

type ProductListItem struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`
	Price       int       `json:"price"`
	Stock       int       `json:"stock"`
	ImageURL    *string   `json:"image_url"`
	ReviewAvg   float64   `json:"review_avg"`
	ReviewCount int       `json:"review_count"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Product struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`
	Description *string   `json:"description"`
	Price       int       `json:"price"`
	Stock       int       `json:"stock"`
	ImageURL    *string   `json:"image_url"`
	SalesCount  int       `json:"sales_count"`
	IsFeatured  bool      `json:"is_featured"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Pagination struct {
	CurrentPage int `json:"currentPage"`
	PerPage     int `json:"perPage"`
	TotalItems  int `json:"totalItems"`
	TotalPages  int `json:"totalPages"`
}

type ProductsPageData struct {
	Products   []ProductListItem `json:"products"`
	Pagination Pagination        `json:"pagination"`
}

type HomePageProduct struct {
	ID          int     `json:"id"`
	Name        string  `json:"name"`
	Price       int     `json:"price"`
	ImageURL    *string `json:"image_url"`
	ReviewAvg   float64 `json:"review_avg"`
	ReviewCount int     `json:"review_count"`
}

type HomePageData struct {
	PickUp     []HomePageProduct `json:"pickUp"`
	NewArrival []HomePageProduct `json:"newArrival"`
	HotItems   []HomePageProduct `json:"hotItems"`
}

func GetProductsHandler(c *gin.Context) {
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 {
		page = 1
	}

	perPage, err := strconv.Atoi(c.DefaultQuery("perPage", "16"))
	if err != nil || perPage < 1 {
		perPage = 16
	}
	if perPage > 100 {
		perPage = 100
	}

	sort := c.DefaultQuery("sort", "new")
	var orderByClause string
	switch sort {
	case "priceAsc":
		orderByClause = "ORDER BY p.price ASC"
	case "new":
		orderByClause = "ORDER BY p.created_at DESC"
	default:
		orderByClause = "ORDER BY p.created_at DESC"
	}

	keyword := c.DefaultQuery("keyword", "")
	var whereClause string
	var whereParams []any
	if keyword != "" {
		whereClause = "WHERE (p.name LIKE ? OR p.description LIKE ?)"
		likeKeyword := "%" + keyword + "%"
		whereParams = append(whereParams, likeKeyword, likeKeyword)
	}

	db := database.GetDB()

	var totalItems int
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM products AS p %s", whereClause)
	if err := db.QueryRow(countQuery, whereParams...).Scan(&totalItems); err != nil {
		log.Printf("商品総数の取得エラー: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		return
	}

	totalPages := int(math.Ceil(float64(totalItems) / float64(perPage)))
	offset := (page - 1) * perPage

	query := fmt.Sprintf(`
		SELECT
			p.id, p.name, p.price, p.stock, p.image_url, p.updated_at,
			COALESCE(ROUND(AVG(r.score), 1), 0.0),
			COUNT(r.id)
		FROM products AS p
		LEFT JOIN reviews AS r ON p.id = r.product_id
		%s
		GROUP BY p.id, p.name, p.price, p.stock, p.image_url, p.updated_at, p.created_at
		%s
		LIMIT ? OFFSET ?
	`, whereClause, orderByClause)

	queryParams := append([]any{}, whereParams...)
	queryParams = append(queryParams, perPage, offset)
	rows, err := db.Query(query, queryParams...)
	if err != nil {
		log.Printf("商品一覧の取得エラー: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		return
	}
	defer rows.Close()

	products := []ProductListItem{}
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
			log.Printf("商品データのスキャンエラー: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
			return
		}
		if imageURL.Valid {
			product.ImageURL = &imageURL.String
		}
		products = append(products, product)
	}
	if err := rows.Err(); err != nil {
		log.Printf("商品一覧データ取得中の行エラー: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		return
	}

	c.JSON(http.StatusOK, ProductsPageData{
		Products: products,
		Pagination: Pagination{
			CurrentPage: page,
			PerPage:     perPage,
			TotalItems:  totalItems,
			TotalPages:  totalPages,
		},
	})
}

func GetProductByIDHandler(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		log.Printf("不正な商品ID形式です: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "商品IDが不正です"})
		return
	}

	db := database.GetDB()
	query := `
		SELECT
			id,
			name,
			description,
			price,
			stock,
			image_url,
			sales_count,
			is_featured,
			created_at,
			updated_at
		FROM products
		WHERE id = ?
	`

	var product Product
	var description sql.NullString
	var imageURL sql.NullString
	err = db.QueryRow(query, id).Scan(
		&product.ID,
		&product.Name,
		&description,
		&product.Price,
		&product.Stock,
		&imageURL,
		&product.SalesCount,
		&product.IsFeatured,
		&product.CreatedAt,
		&product.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Printf("商品が見つかりません: ID=%d", id)
			c.JSON(http.StatusNotFound, gin.H{"error": "商品が見つかりませんでした"})
		} else {
			log.Printf("商品取得エラー (ID=%d): %v", id, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		}
		return
	}

	if description.Valid {
		product.Description = &description.String
	}
	if imageURL.Valid {
		product.ImageURL = &imageURL.String
	}

	c.JSON(http.StatusOK, product)
}

func GetHomePageProductsHandler(c *gin.Context) {
	db := database.GetDB()

	var pickUp, newArrival, hotItems []HomePageProduct
	var pickUpErr, newArrivalErr, hotItemsErr error
	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		rows, err := db.Query(`
			SELECT id, name, price, image_url
			FROM products
			ORDER BY sales_count DESC
			LIMIT 3
		`)
		if err != nil {
			log.Printf("おすすめ商品取得エラー: %v", err)
			pickUpErr = err
			return
		}
		defer rows.Close()

		for rows.Next() {
			var product HomePageProduct
			var imageURL sql.NullString
			if err := rows.Scan(&product.ID, &product.Name, &product.Price, &imageURL); err != nil {
				log.Printf("おすすめ商品スキャンエラー: %v", err)
				pickUpErr = err
				return
			}
			if imageURL.Valid {
				product.ImageURL = &imageURL.String
			}
			product.ReviewAvg = 0
			product.ReviewCount = 0
			pickUp = append(pickUp, product)
		}
		pickUpErr = rows.Err()
	}()

	go func() {
		defer wg.Done()
		rows, err := db.Query(`
			SELECT
				p.id, p.name, p.price, p.image_url,
				COALESCE(ROUND(AVG(r.score), 1), 0.0),
				COUNT(r.id)
			FROM products AS p
			LEFT JOIN reviews AS r ON p.id = r.product_id
			GROUP BY p.id, p.name, p.price, p.image_url, p.created_at
			ORDER BY p.created_at DESC
			LIMIT 4
		`)
		if err != nil {
			log.Printf("新着商品取得エラー: %v", err)
			newArrivalErr = err
			return
		}
		defer rows.Close()

		for rows.Next() {
			var product HomePageProduct
			var imageURL sql.NullString
			if err := rows.Scan(
				&product.ID,
				&product.Name,
				&product.Price,
				&imageURL,
				&product.ReviewAvg,
				&product.ReviewCount,
			); err != nil {
				log.Printf("新着商品スキャンエラー: %v", err)
				newArrivalErr = err
				return
			}
			if imageURL.Valid {
				product.ImageURL = &imageURL.String
			}
			newArrival = append(newArrival, product)
		}
		newArrivalErr = rows.Err()
	}()

	go func() {
		defer wg.Done()
		rows, err := db.Query(`
			SELECT
				p.id, p.name, p.price, p.image_url,
				COALESCE(ROUND(AVG(r.score), 1), 0.0),
				COUNT(r.id)
			FROM products AS p
			LEFT JOIN reviews AS r ON p.id = r.product_id
			WHERE p.is_featured = TRUE
			GROUP BY p.id, p.name, p.price, p.image_url
			ORDER BY RAND()
			LIMIT 4
		`)
		if err != nil {
			log.Printf("注目商品取得エラー: %v", err)
			hotItemsErr = err
			return
		}
		defer rows.Close()

		for rows.Next() {
			var product HomePageProduct
			var imageURL sql.NullString
			if err := rows.Scan(
				&product.ID,
				&product.Name,
				&product.Price,
				&imageURL,
				&product.ReviewAvg,
				&product.ReviewCount,
			); err != nil {
				log.Printf("注目商品スキャンエラー: %v", err)
				hotItemsErr = err
				return
			}
			if imageURL.Valid {
				product.ImageURL = &imageURL.String
			}
			hotItems = append(hotItems, product)
		}
		hotItemsErr = rows.Err()
	}()

	wg.Wait()
	if pickUpErr != nil || newArrivalErr != nil || hotItemsErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		return
	}

	c.JSON(http.StatusOK, HomePageData{
		PickUp:     pickUp,
		NewArrival: newArrival,
		HotItems:   hotItems,
	})
}
