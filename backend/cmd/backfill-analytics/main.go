// Command backfill-analytics rebuilds analytics rollup days and
// backfills payer hashes for pre-analytics orders.
//
// Rollups are a cache: refreshing a date range recomputes it wholesale
// (delete + insert per EAT day), so reruns are idempotent. Payer-hash
// backfill runs in bounded batches — rerun until it reports 0 remaining.
//
//	go run ./cmd/backfill-analytics -from 2026-09-01 -to 2026-09-28
//	go run ./cmd/backfill-analytics -payer-hash-backfill -batch 1000
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"time"

	"lipago/internal/modules/payments"
	"lipago/internal/modules/payments/providers"
	"lipago/internal/platform/config"
	azcrypto "lipago/internal/platform/crypto"
	"lipago/internal/platform/database"
)

func main() {
	var (
		fromFlag  = flag.String("from", "", "first EAT day to rebuild (YYYY-MM-DD, default yesterday)")
		toFlag    = flag.String("to", "", "last EAT day to rebuild (YYYY-MM-DD, default today)")
		payerBack = flag.Bool("payer-hash-backfill", false, "also backfill payer_hash on pre-analytics orders")
		batch     = flag.Int("batch", 500, "payer-hash rows per batch")
		dbURLFlag = flag.String("database-url", os.Getenv("DATABASE_URL"), "postgres connection string")
	)
	flag.Parse()

	now := time.Now().UTC()
	from := now.Add(-24 * time.Hour)
	to := now
	var err error
	if *fromFlag != "" {
		if from, err = time.Parse("2006-01-02", *fromFlag); err != nil {
			log.Fatalf("bad -from date: %v", err)
		}
	}
	if *toFlag != "" {
		if to, err = time.Parse("2006-01-02", *toFlag); err != nil {
			log.Fatalf("bad -to date: %v", err)
		}
	}
	if to.Before(from) {
		log.Fatal("-to is before -from")
	}
	if *dbURLFlag == "" {
		config.LoadDotEnv(".env")
		*dbURLFlag = os.Getenv("DATABASE_URL")
	}
	if *dbURLFlag == "" {
		log.Fatal("DATABASE_URL or -database-url is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	db, err := database.NewPool(ctx, cfg.Database)
	if err != nil {
		log.Fatalf("connect database: %v", err)
	}
	defer db.Close()

	cipher, err := azcrypto.New(cfg.Security.EncryptionKey)
	if err != nil {
		log.Fatalf("init cipher: %v", err)
	}
	service := payments.NewService(payments.NewPostgresRepository(db), providers.Registry, cipher, payments.ServiceOptions{
		PayerHashSecret: cfg.Payments.PayerHashSecret,
	}, nil)

	if err := service.RefreshAnalyticsRollups(ctx, from, to); err != nil {
		log.Fatalf("refresh rollups: %v", err)
	}
	log.Printf("rebuilt analytics rollups %s..%s", from.Format("2006-01-02"), to.Format("2006-01-02"))

	if *payerBack {
		for {
			done, err := service.BackfillPayerHashes(ctx, *batch)
			if err != nil {
				log.Fatalf("backfill payer hashes: %v", err)
			}
			log.Printf("hashed %d orders", done)
			if done < int64(*batch) {
				break
			}
		}
	}
}
