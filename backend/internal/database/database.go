package database

import (
	"database/sql"
	"log"
	"os"

	_ "github.com/go-sql-driver/mysql"
)

var db *sql.DB

func InitDB() {
	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		log.Fatal("エラー: 環境変数DB_DSNが設定されていません")
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
