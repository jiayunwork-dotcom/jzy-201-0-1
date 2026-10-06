package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"microwaveplanner/internal/assessment"
	"microwaveplanner/internal/httpapi"
	"microwaveplanner/internal/store"
	"microwaveplanner/internal/task"
)

func main() {
	dsn := os.Getenv("MYSQL_DSN")
	if dsn == "" {
		dsn = "root:planner@tcp(mysql:3306)/microwave?charset=utf8mb4&collation=utf8mb4_unicode_ci"
	}
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	st, err := store.New(dsn)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	as := assessment.New(st)
	tm := task.NewManager(st, as)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	tm.Start(ctx)

	router := httpapi.NewRouter(&httpapi.Server{Store: st, Assessment: as, Tasks: tm})
	srv := &http.Server{Addr: addr, Handler: router, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		log.Printf("microwave planner HTTP service listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server: %v", err)
		}
	}()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}
