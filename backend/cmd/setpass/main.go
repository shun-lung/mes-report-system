// setpass 重設使用者密碼。
//
//	go run ./cmd/setpass -email pc@demo.local -password '新密碼'
package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"golang.org/x/crypto/bcrypt"

	"mes/internal/config"
	"mes/internal/database"
)

func main() {
	email := flag.String("email", "", "使用者 email")
	password := flag.String("password", "", "新密碼（至少 8 碼）")
	flag.Parse()
	if *email == "" || len(*password) < 8 {
		log.Fatal("用法：setpass -email <email> -password <至少 8 碼的新密碼>")
	}

	cfg := config.Load()
	ctx := context.Background()
	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	hash, err := bcrypt.GenerateFromPassword([]byte(*password), bcrypt.DefaultCost)
	if err != nil {
		log.Fatal(err)
	}
	tag, err := pool.Exec(ctx, `UPDATE users SET password_hash = $2, updated_at = now() WHERE email = $1`, *email, string(hash))
	if err != nil {
		log.Fatal(err)
	}
	if tag.RowsAffected() == 0 {
		log.Fatalf("找不到使用者 %s", *email)
	}
	fmt.Printf("已更新 %s 的密碼\n", *email)
}
