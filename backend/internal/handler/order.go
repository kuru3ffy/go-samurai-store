package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stripe/stripe-go/v83"
	"github.com/stripe/stripe-go/v83/checkout/session"
	"github.com/stripe/stripe-go/v83/webhook"

	"github.com/kuru3ffy/go-samurai-store/backend/internal/database"
)

type CartItem struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Price    int    `json:"price"`
	ImageURL string `json:"imageUrl"`
	Quantity int    `json:"quantity"`
}

type CheckoutRequest struct {
	Items   []CartItem `json:"items" binding:"required"`
	Address string     `json:"address" binding:"required"`
}

type productForOrder struct {
	ID    int
	Name  string
	Price int
	Stock int
}

type OrderItem struct {
	ProductName string `json:"productName"`
	Quantity    int    `json:"quantity"`
	UnitPrice   int    `json:"unitPrice"`
}

type OrderData struct {
	ID            int         `json:"id"`
	TotalPrice    int         `json:"totalPrice"`
	Status        string      `json:"status"`
	PaymentStatus string      `json:"paymentStatus"`
	CreatedAt     time.Time   `json:"createdAt"`
	Items         []OrderItem `json:"items"`
}

type orderJoinRecord struct {
	ID            int
	TotalPrice    int
	Status        string
	PaymentStatus string
	CreatedAt     time.Time
	ProductName   string
	Quantity      int
	UnitPrice     int
}

const (
	OrderStatusPending    = "未処理"
	OrderStatusProcessing = "処理中"
	PaymentStatusUnpaid   = "未決済"
	PaymentStatusPaid     = "決済成功"
	shippingCost          = 500
)

func init() {
	stripe.Key = os.Getenv("STRIPE_SECRET_KEY")
	if stripe.Key == "" {
		log.Println("警告: 環境変数 STRIPE_SECRET_KEY が設定されていません")
	}
}

func CreateCheckoutSessionHandler(c *gin.Context) {
	var req CheckoutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("Checkoutリクエストのバインドエラー: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "リクエスト内容が正しくありません"})
		return
	}
	if len(req.Items) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "カートが空です"})
		return
	}
	req.Address = strings.TrimSpace(req.Address)
	if req.Address == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "お届け先住所を入力してください"})
		return
	}

	userClaims, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "認証が必要です"})
		return
	}
	claims, ok := userClaims.(*JWTCustomClaims)
	if !ok {
		log.Println("CreateCheckoutSessionHandler: ユーザー情報の型アサーションに失敗しました")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		return
	}

	productIDs := make([]any, 0, len(req.Items))
	quantityMap := make(map[int]int, len(req.Items))
	orderedIDs := make([]int, 0, len(req.Items))
	for _, item := range req.Items {
		id, err := strconv.Atoi(item.ID)
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "カート内の商品IDが不正です"})
			return
		}
		if item.Quantity <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "商品数量は1以上で指定してください"})
			return
		}
		if _, duplicated := quantityMap[id]; duplicated {
			c.JSON(http.StatusBadRequest, gin.H{"error": "同じ商品が重複しています"})
			return
		}
		productIDs = append(productIDs, id)
		orderedIDs = append(orderedIDs, id)
		quantityMap[id] = item.Quantity
	}

	placeholders := strings.Repeat("?,", len(productIDs)-1) + "?"
	query := fmt.Sprintf("SELECT id, name, price, stock FROM products WHERE id IN (%s)", placeholders)
	rows, err := database.GetDB().Query(query, productIDs...)
	if err != nil {
		log.Printf("在庫チェック時の商品取得エラー: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		return
	}
	defer rows.Close()

	dbProducts := make(map[int]productForOrder, len(productIDs))
	shortageItems := make([]string, 0)
	for rows.Next() {
		var product productForOrder
		if err := rows.Scan(&product.ID, &product.Name, &product.Price, &product.Stock); err != nil {
			log.Printf("在庫チェック時の商品スキャンエラー: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
			return
		}
		dbProducts[product.ID] = product
		if product.Stock < quantityMap[product.ID] {
			shortageItems = append(shortageItems, product.Name)
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("在庫チェック時の行エラー: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		return
	}
	if len(dbProducts) != len(productIDs) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "カート内の一部商品が見つかりませんでした"})
		return
	}
	if len(shortageItems) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("在庫不足の商品があります: %s", strings.Join(shortageItems, "、"))})
		return
	}

	tx, err := database.GetDB().Begin()
	if err != nil {
		log.Printf("トランザクション開始エラー: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		return
	}
	defer tx.Rollback()

	totalPrice := shippingCost
	for _, id := range orderedIDs {
		totalPrice += dbProducts[id].Price * quantityMap[id]
	}
	result, err := tx.Exec(`
		INSERT INTO orders (user_id, total_price, status, payment_status, shipping_address)
		VALUES (?, ?, ?, ?, ?)
	`, claims.UserID, totalPrice, OrderStatusPending, PaymentStatusUnpaid, req.Address)
	if err != nil {
		log.Printf("注文登録エラー: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "注文の登録に失敗しました"})
		return
	}
	orderID, err := result.LastInsertId()
	if err != nil {
		log.Printf("注文ID取得エラー: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "注文の登録に失敗しました"})
		return
	}

	stmt, err := tx.Prepare(`
		INSERT INTO order_items (order_id, product_id, product_name, quantity, unit_price)
		VALUES (?, ?, ?, ?, ?)
	`)
	if err != nil {
		log.Printf("注文明細INSERT準備エラー: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "注文の登録に失敗しました"})
		return
	}
	defer stmt.Close()
	for _, id := range orderedIDs {
		product := dbProducts[id]
		if _, err := stmt.Exec(orderID, product.ID, product.Name, quantityMap[id], product.Price); err != nil {
			log.Printf("注文明細INSERT実行エラー: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "注文の登録に失敗しました"})
			return
		}
	}

	lineItems := make([]*stripe.CheckoutSessionLineItemParams, 0, len(orderedIDs)+1)
	for _, id := range orderedIDs {
		product := dbProducts[id]
		lineItems = append(lineItems, &stripe.CheckoutSessionLineItemParams{
			PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
				Currency: stripe.String(string(stripe.CurrencyJPY)),
				ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
					Name: stripe.String(product.Name),
				},
				UnitAmount: stripe.Int64(int64(product.Price)),
			},
			Quantity: stripe.Int64(int64(quantityMap[id])),
		})
	}
	lineItems = append(lineItems, &stripe.CheckoutSessionLineItemParams{
		PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
			Currency: stripe.String(string(stripe.CurrencyJPY)),
			ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
				Name: stripe.String("送料"),
			},
			UnitAmount: stripe.Int64(shippingCost),
		},
		Quantity: stripe.Int64(1),
	})

	params := &stripe.CheckoutSessionParams{
		LineItems:     lineItems,
		Mode:          stripe.String(string(stripe.CheckoutSessionModePayment)),
		SuccessURL:    stripe.String("http://localhost:3000/account?session_id={CHECKOUT_SESSION_ID}"),
		CancelURL:     stripe.String("http://localhost:3000/order-confirm"),
		CustomerEmail: stripe.String(claims.Email),
		Metadata: map[string]string{
			"orderId": strconv.FormatInt(orderID, 10),
			"userId":  strconv.Itoa(claims.UserID),
		},
	}
	checkoutSession, err := session.New(params)
	if err != nil {
		log.Printf("Stripe Checkoutセッション作成エラー: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "決済ページの生成に失敗しました"})
		return
	}
	if err := tx.Commit(); err != nil {
		log.Printf("トランザクションコミットエラー: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "注文の確定に失敗しました"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"url": checkoutSession.URL})
}

func StripeWebhookHandler(c *gin.Context) {
	webhookSecret := os.Getenv("STRIPE_WEBHOOK_SECRET")
	if webhookSecret == "" {
		log.Println("警告: 環境変数 STRIPE_WEBHOOK_SECRET が設定されていません")
		c.Status(http.StatusInternalServerError)
		return
	}
	payload, err := io.ReadAll(c.Request.Body)
	if err != nil {
		log.Printf("Webhookリクエストボディ読み込みエラー: %v", err)
		c.Status(http.StatusBadRequest)
		return
	}
	event, err := webhook.ConstructEventWithOptions(
		payload,
		c.GetHeader("Stripe-Signature"),
		webhookSecret,
		webhook.ConstructEventOptions{IgnoreAPIVersionMismatch: true},
	)
	if err != nil {
		log.Printf("Webhook署名検証エラー: %v", err)
		c.Status(http.StatusBadRequest)
		return
	}

	if event.Type != "checkout.session.completed" {
		log.Printf("Webhook受信（無視するイベント）: %s", event.Type)
		c.Status(http.StatusOK)
		return
	}

	var checkoutSession stripe.CheckoutSession
	if err := json.Unmarshal(event.Data.Raw, &checkoutSession); err != nil {
		log.Printf("Webhookイベントデータ解析エラー: %v", err)
		c.Status(http.StatusBadRequest)
		return
	}
	orderIDText, hasOrderID := checkoutSession.Metadata["orderId"]
	userIDText, hasUserID := checkoutSession.Metadata["userId"]
	if !hasOrderID || !hasUserID {
		log.Println("Webhookメタデータ不足")
		c.Status(http.StatusBadRequest)
		return
	}
	orderID, orderIDErr := strconv.ParseInt(orderIDText, 10, 64)
	userID, userIDErr := strconv.Atoi(userIDText)
	if orderIDErr != nil || userIDErr != nil {
		log.Printf("Webhookメタデータ形式エラー: orderId=%s, userId=%s", orderIDText, userIDText)
		c.Status(http.StatusBadRequest)
		return
	}

	tx, err := database.GetDB().Begin()
	if err != nil {
		log.Printf("Webhookトランザクション開始エラー: %v", err)
		c.Status(http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	result, err := tx.Exec(`
		UPDATE orders
		SET status = ?, payment_status = ?
		WHERE id = ? AND user_id = ? AND payment_status != ?
	`, OrderStatusProcessing, PaymentStatusPaid, orderID, userID, PaymentStatusPaid)
	if err != nil {
		log.Printf("Webhook注文ステータス更新エラー: %v", err)
		c.Status(http.StatusInternalServerError)
		return
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		log.Printf("Webhook更新件数取得エラー: %v", err)
		c.Status(http.StatusInternalServerError)
		return
	}
	if rowsAffected == 0 {
		c.Status(http.StatusOK)
		return
	}

	rows, err := tx.Query("SELECT product_id, quantity FROM order_items WHERE order_id = ?", orderID)
	if err != nil {
		log.Printf("Webhook注文明細取得エラー: %v", err)
		c.Status(http.StatusInternalServerError)
		return
	}
	type orderItem struct {
		ProductID int
		Quantity  int
	}
	items := make([]orderItem, 0)
	for rows.Next() {
		var item orderItem
		if err := rows.Scan(&item.ProductID, &item.Quantity); err != nil {
			rows.Close()
			log.Printf("Webhook注文明細スキャンエラー: %v", err)
			c.Status(http.StatusInternalServerError)
			return
		}
		items = append(items, item)
	}
	rowsErr := rows.Err()
	rows.Close()
	if rowsErr != nil {
		log.Printf("Webhook注文明細行エラー: %v", rowsErr)
		c.Status(http.StatusInternalServerError)
		return
	}
	if len(items) == 0 {
		log.Printf("Webhook注文明細がありません: OrderID=%d", orderID)
		c.Status(http.StatusInternalServerError)
		return
	}

	stmt, err := tx.Prepare(`
		UPDATE products
		SET stock = stock - ?, sales_count = sales_count + ?
		WHERE id = ? AND stock >= ?
	`)
	if err != nil {
		log.Printf("Webhook在庫更新準備エラー: %v", err)
		c.Status(http.StatusInternalServerError)
		return
	}
	defer stmt.Close()
	for _, item := range items {
		result, err := stmt.Exec(item.Quantity, item.Quantity, item.ProductID, item.Quantity)
		if err != nil {
			log.Printf("Webhook在庫更新実行エラー: %v", err)
			c.Status(http.StatusInternalServerError)
			return
		}
		affected, err := result.RowsAffected()
		if err != nil || affected == 0 {
			log.Printf("Webhook在庫不足または商品なし: ProductID=%d", item.ProductID)
			c.Status(http.StatusInternalServerError)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		log.Printf("Webhookトランザクションコミットエラー: %v", err)
		c.Status(http.StatusInternalServerError)
		return
	}

	log.Printf("Webhook処理成功: OrderID=%d", orderID)
	c.Status(http.StatusOK)
}

func GetOrdersHandler(c *gin.Context) {
	userClaims, exists := c.Get("user")
	if !exists {
		log.Println("GetOrdersHandler: ユーザー情報がコンテキストにありません")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "認証が必要です"})
		return
	}
	claims, ok := userClaims.(*JWTCustomClaims)
	if !ok {
		log.Println("GetOrdersHandler: ユーザー情報の型アサーションに失敗しました")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		return
	}

	rows, err := database.GetDB().Query(`
		SELECT
			o.id, o.total_price, o.status, o.payment_status, o.created_at,
			oi.product_name, oi.quantity, oi.unit_price
		FROM orders AS o
		JOIN order_items AS oi ON o.id = oi.order_id
		WHERE o.user_id = ?
		ORDER BY o.created_at DESC, o.id DESC, oi.id ASC
	`, claims.UserID)
	if err != nil {
		log.Printf("注文履歴取得エラー (UserID=%d): %v", claims.UserID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		return
	}
	defer rows.Close()

	ordersMap := make(map[int]*OrderData)
	for rows.Next() {
		var record orderJoinRecord
		if err := rows.Scan(
			&record.ID,
			&record.TotalPrice,
			&record.Status,
			&record.PaymentStatus,
			&record.CreatedAt,
			&record.ProductName,
			&record.Quantity,
			&record.UnitPrice,
		); err != nil {
			log.Printf("注文履歴スキャンエラー (UserID=%d): %v", claims.UserID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
			return
		}

		order, exists := ordersMap[record.ID]
		if !exists {
			order = &OrderData{
				ID:            record.ID,
				TotalPrice:    record.TotalPrice,
				Status:        record.Status,
				PaymentStatus: record.PaymentStatus,
				CreatedAt:     record.CreatedAt,
				Items:         make([]OrderItem, 0),
			}
			ordersMap[record.ID] = order
		}
		order.Items = append(order.Items, OrderItem{
			ProductName: record.ProductName,
			Quantity:    record.Quantity,
			UnitPrice:   record.UnitPrice,
		})
	}
	if err := rows.Err(); err != nil {
		log.Printf("注文履歴行エラー (UserID=%d): %v", claims.UserID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "サーバーエラーが発生しました"})
		return
	}

	orders := make([]OrderData, 0, len(ordersMap))
	for _, order := range ordersMap {
		orders = append(orders, *order)
	}
	sort.Slice(orders, func(i, j int) bool {
		if orders[i].CreatedAt.Equal(orders[j].CreatedAt) {
			return orders[i].ID > orders[j].ID
		}
		return orders[i].CreatedAt.After(orders[j].CreatedAt)
	})

	c.JSON(http.StatusOK, gin.H{"orders": orders})
}
