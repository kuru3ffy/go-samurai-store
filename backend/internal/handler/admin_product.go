package handler

import (
	"database/sql"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kuru3ffy/go-samurai-store/backend/internal/database"
)

const maxProductImageSize = 5 << 20

func AdminCreateProductHandler(c *gin.Context) {
	name := c.PostForm("name")
	description := c.PostForm("description")
	priceString := c.PostForm("price")
	stockString := c.PostForm("stock")
	isFeatured := c.PostForm("isFeatured") == "on"

	fileHeader, err := c.FormFile("imageFile")
	if err != nil {
		log.Printf("画像ファイルの取得エラー: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "商品画像は必須です"})
		return
	}

	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "商品名は必須です"})
		return
	}
	price, err := strconv.Atoi(priceString)
	if err != nil || price < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "価格は0以上の整数で入力してください"})
		return
	}
	stock, err := strconv.Atoi(stockString)
	if err != nil || stock < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "在庫数は0以上の整数で入力してください"})
		return
	}
	if description == "" {
		description = "商品の説明がありません。"
	}
	if fileHeader.Size > maxProductImageSize {
		c.JSON(http.StatusBadRequest, gin.H{"error": "商品画像は5MB以下にしてください"})
		return
	}

	extension := strings.ToLower(filepath.Ext(fileHeader.Filename))
	allowedExtensions := map[string]bool{".jpg": true, ".jpeg": true, ".png": true}
	if !allowedExtensions[extension] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "対応していないファイル形式です（jpg, jpeg, pngのみ）"})
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		log.Printf("画像ファイルを開けませんでした: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "商品画像を読み込めませんでした"})
		return
	}
	defer file.Close()
	header := make([]byte, 512)
	readSize, err := file.Read(header)
	if err != nil {
		log.Printf("画像ファイルの読み込みエラー: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "商品画像を読み込めませんでした"})
		return
	}
	contentType := http.DetectContentType(header[:readSize])
	allowedContentTypes := map[string]bool{"image/jpeg": true, "image/png": true}
	if !allowedContentTypes[contentType] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "画像ファイルの内容が不正です"})
		return
	}

	timestamp := time.Now().UnixMilli()
	fileName := fmt.Sprintf("%d_%d%s", timestamp, rand.Intn(10000), extension)
	savePath := filepath.Join("uploads", fileName)
	if err := c.SaveUploadedFile(fileHeader, savePath); err != nil {
		log.Printf("画像ファイルの保存エラー: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ファイルのアップロードに失敗しました"})
		return
	}

	db := database.GetDB()
	_, err = db.Exec(`
		INSERT INTO products (name, description, price, stock, image_url, is_featured)
		VALUES (?, ?, ?, ?, ?, ?)
	`, name, description, price, stock, fileName, isFeatured)
	if err != nil {
		log.Printf("商品登録エラー: %v", err)
		_ = os.Remove(savePath)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		return
	}

	log.Printf("商品と画像を登録しました: %s", savePath)
	c.JSON(http.StatusCreated, gin.H{"message": "商品を登録しました"})
}

func AdminUpdateProductHandler(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		log.Printf("不正な商品ID形式です: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "商品IDが不正です"})
		return
	}

	db := database.GetDB()
	var currentImageURL sql.NullString
	if err := db.QueryRow("SELECT image_url FROM products WHERE id = ?", id).Scan(&currentImageURL); err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "商品が見つかりませんでした"})
		} else {
			log.Printf("既存商品チェックエラー (ID=%d): %v", id, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		}
		return
	}

	name := c.PostForm("name")
	description := c.PostForm("description")
	priceString := c.PostForm("price")
	stockString := c.PostForm("stock")
	isFeatured := c.PostForm("isFeatured") == "on"

	fileHeader, fileErr := c.FormFile("imageFile")
	if fileErr != nil && fileErr != http.ErrMissingFile {
		log.Printf("画像ファイルの取得エラー: %v", fileErr)
		c.JSON(http.StatusBadRequest, gin.H{"error": "商品画像を読み込めませんでした"})
		return
	}

	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "商品名は必須です"})
		return
	}
	price, err := strconv.Atoi(priceString)
	if err != nil || price < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "価格は0以上の整数で入力してください"})
		return
	}
	stock, err := strconv.Atoi(stockString)
	if err != nil || stock < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "在庫数は0以上の整数で入力してください"})
		return
	}
	if description == "" {
		description = "商品の説明がありません。"
	}

	imageURLToSave := currentImageURL.String
	oldFileName := currentImageURL.String
	newFileName := ""
	if fileHeader != nil {
		if fileHeader.Size > maxProductImageSize {
			c.JSON(http.StatusBadRequest, gin.H{"error": "商品画像は5MB以下にしてください"})
			return
		}

		extension := strings.ToLower(filepath.Ext(fileHeader.Filename))
		allowedExtensions := map[string]bool{".jpg": true, ".jpeg": true, ".png": true}
		if !allowedExtensions[extension] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "対応していないファイル形式です（jpg, jpeg, pngのみ）"})
			return
		}

		file, err := fileHeader.Open()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "商品画像を読み込めませんでした"})
			return
		}
		header := make([]byte, 512)
		readSize, readErr := file.Read(header)
		_ = file.Close()
		if readErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "商品画像を読み込めませんでした"})
			return
		}
		contentType := http.DetectContentType(header[:readSize])
		allowedContentTypes := map[string]bool{"image/jpeg": true, "image/png": true}
		if !allowedContentTypes[contentType] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "画像ファイルの内容が不正です"})
			return
		}

		newFileName = fmt.Sprintf("%d_%d%s", time.Now().UnixMilli(), rand.Intn(10000), extension)
		if err := c.SaveUploadedFile(fileHeader, filepath.Join("uploads", newFileName)); err != nil {
			log.Printf("新しい画像ファイルの保存エラー: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "ファイルのアップロードに失敗しました"})
			return
		}
		imageURLToSave = newFileName
	}

	_, err = db.Exec(`
		UPDATE products SET
			name = ?, description = ?, price = ?, stock = ?, image_url = ?, is_featured = ?
		WHERE id = ?
	`,
		name,
		description,
		price,
		stock,
		sql.NullString{String: imageURLToSave, Valid: imageURLToSave != ""},
		isFeatured,
		id,
	)
	if err != nil {
		log.Printf("商品更新エラー (ID=%d): %v", id, err)
		if newFileName != "" {
			_ = os.Remove(filepath.Join("uploads", newFileName))
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		return
	}

	if newFileName != "" && oldFileName != "" && oldFileName != newFileName {
		var references int
		if err := db.QueryRow("SELECT COUNT(*) FROM products WHERE image_url = ?", oldFileName).Scan(&references); err != nil {
			log.Printf("旧画像の参照数取得エラー: %v", err)
		} else if references == 0 {
			if err := os.Remove(filepath.Join("uploads", oldFileName)); err != nil && !os.IsNotExist(err) {
				log.Printf("古い画像ファイルの削除エラー: %v", err)
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "商品を編集しました"})
}

func AdminDeleteProductHandler(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		log.Printf("不正な商品ID形式です: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "商品IDが不正です"})
		return
	}

	db := database.GetDB()
	var imageURL sql.NullString
	if err := db.QueryRow("SELECT image_url FROM products WHERE id = ?", id).Scan(&imageURL); err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "商品が見つかりませんでした"})
		} else {
			log.Printf("既存商品チェックエラー (ID=%d): %v", id, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		}
		return
	}

	if _, err := db.Exec("DELETE FROM products WHERE id = ?", id); err != nil {
		log.Printf("商品削除エラー (ID=%d): %v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		return
	}

	if imageURL.Valid && imageURL.String != "" {
		var references int
		if err := db.QueryRow("SELECT COUNT(*) FROM products WHERE image_url = ?", imageURL.String).Scan(&references); err != nil {
			log.Printf("画像参照数の取得エラー: %v", err)
		} else if references == 0 {
			filePath := filepath.Join("uploads", imageURL.String)
			if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
				log.Printf("画像ファイルの削除エラー: %v", err)
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "商品を削除しました"})
}
