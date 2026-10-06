package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"microwave-network/internal/api"
	"microwave-network/internal/service"
	"microwave-network/internal/store"
)

func main() {
	dsn := os.Getenv("MYSQL_DSN")
	if dsn == "" {
		dsn = "microwave:microwave@tcp(mysql:3306)/microwave"
	}
	ms, err := store.OpenMySQL(dsn)
	if err != nil {
		log.Fatalf("open mysql: %v", err)
	}
	defer ms.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := ms.Migrate(ctx); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	app := service.NewApp(ms)
	backgroundCtx, stop := context.WithCancel(context.Background())
	defer stop()
	app.Tasks.StartWorker(backgroundCtx, time.Second)
	srv := &http.Server{Addr: ":" + getenv("PORT", "8080"), Handler: api.New(app)}
	log.Printf("listening on %s", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
