package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/jackc/pgx/v5/pgxpool"

	"stockflow/internal/httpapi"
	"stockflow/internal/store"
)

func main() {
	dsn := getenv("DATABASE_URL", "postgres://stockflow:stockflow@localhost:5432/stockflow?sslmode=disable")
	addr := getenv("ADDR", ":8080")

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	for i := 0; ; i++ {
		if err = pool.Ping(ctx); err == nil {
			break
		}
		if i >= 30 {
			log.Fatalf("database unreachable: %v", err)
		}
		time.Sleep(time.Second)
	}

	st := store.New(pool)
	srv := &http.Server{Addr: addr, Handler: httpapi.New(st), ReadHeaderTimeout: 5 * time.Second}

	stop, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-stop.Done()

		sctx, scancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer scancel()
		if err := srv.Shutdown(sctx); err != nil {
			log.Printf("shutdown: %v", err)
		}
		if err := st.Close(sctx); err != nil {
			log.Printf("audit flush on shutdown: %v", err)
		}
	}()

	log.Printf("stockflow api listening on %s", addr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	<-done
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
