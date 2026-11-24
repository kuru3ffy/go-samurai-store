package database

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "github.com/go-sql-driver/mysql"
)

var db *sql.DB

func InitDB() {
	dbUser := os.Getenv("DB_USER")
	dbPass := os.Getenv("DB_PASSWORD")
	dbName := os.Getenv("DB_NAME")
	dbSocketPath := os.Getenv("DB_SOCKET_PATH")

	var dsn string
	if dbSocketPath != "" {
		dsn = fmt.Sprintf(
			"%s:%s@unix(%s)/%s?charset=utf8mb4&parseTime=true&loc=Local",
			dbUser,
			dbPass,
			dbSocketPath,
			dbName,
		)
	} else {
		dsn = os.Getenv("DB_DSN")
		if dsn == "" {
			log.Fatal("エラー: 環境変数DB_DSN（開発環境）またはDB_SOCKET_PATH（本番環境）が設定されていません")
		}
	}

	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("データベースへの接続準備に失敗しました: %v", err)
	}

	if err := conn.Ping(); err != nil {
		log.Fatalf("データベースへの接続に失敗しました: %v", err)
	}

	log.Println("データベースへの接続に成功しました！")
	db = conn
}

func GetDB() *sql.DB {
	if db == nil {
		log.Fatal("データベースが初期化されていません")
	}
	return db
}
