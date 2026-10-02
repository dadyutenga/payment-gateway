package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx"
	"lipago/internal/platform/config"
	"lipago/internal/platform/database"
	"lipago/internal/platform/storage"
)

var documentExtensions = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".pdf": true,
}

var imageExtensions = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".webp": true,
}

func main() {
	root := flag.String("root", ".", "directory containing legacy uploads/ paths")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	pool, err := database.NewPool(ctx, cfg.Database)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	store, err := storage.NewR2(ctx, storage.R2Config{
		AccessKeyID: cfg.Storage.R2AccessKeyID,
		SecretKey:   cfg.Storage.R2SecretKey,
		Bucket:      cfg.Storage.R2Bucket,
		Endpoint:    cfg.Storage.R2Endpoint,
	})
	if err != nil {
		log.Fatal(err)
	}

	if err := migrateKYC(ctx, pool, store, *root); err != nil {
		log.Fatal(err)
	}
	if err := migrateLogos(ctx, pool, store, *root); err != nil {
		log.Fatal(err)
	}
	log.Print("upload migration complete; local files were retained for manual cleanup")
}

func migrateKYC(ctx context.Context, pool *pgx.ConnPool, store storage.Store, root string) error {
	rows, err := pool.QueryEx(ctx, `SELECT org_id::text, id_document_url, id_document_back_url, selfie_url FROM app.kyc_submissions`, nil)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var orgID, document, back, selfie string
		if err := rows.Scan(&orgID, &document, &back, &selfie); err != nil {
			return err
		}
		newDocument, err := migrateFile(ctx, store, root, document, "private/kyc/"+orgID, "document", 5<<20, documentExtensions)
		if err != nil {
			return fmt.Errorf("KYC document for %s: %w", orgID, err)
		}
		newBack, err := migrateFile(ctx, store, root, back, "private/kyc/"+orgID, "document-back", 5<<20, documentExtensions)
		if err != nil {
			return fmt.Errorf("KYC back document for %s: %w", orgID, err)
		}
		newSelfie, err := migrateFile(ctx, store, root, selfie, "private/kyc/"+orgID, "selfie", 5<<20, imageExtensions)
		if err != nil {
			return fmt.Errorf("KYC selfie for %s: %w", orgID, err)
		}
		if newDocument != document || newBack != back || newSelfie != selfie {
			if _, err := pool.ExecEx(ctx, `UPDATE app.kyc_submissions SET id_document_url = $2, id_document_back_url = $3, selfie_url = $4 WHERE org_id = $1::uuid`, nil, orgID, newDocument, newBack, newSelfie); err != nil {
				return err
			}
			log.Printf("migrated KYC objects for org %s", orgID)
		}
	}
	return rows.Err()
}

func migrateLogos(ctx context.Context, pool *pgx.ConnPool, store storage.Store, root string) error {
	rows, err := pool.QueryEx(ctx, `SELECT id::text, logo_url FROM app.organizations WHERE COALESCE(logo_url, '') <> ''`, nil)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var orgID, logo string
		if err := rows.Scan(&orgID, &logo); err != nil {
			return err
		}
		migrated, err := migrateFile(ctx, store, root, logo, "public/branding", orgID, 2<<20, imageExtensions)
		if err != nil {
			return fmt.Errorf("logo for org %s: %w", orgID, err)
		}
		if migrated != logo {
			if _, err := pool.ExecEx(ctx, `UPDATE app.organizations SET logo_url = $2, updated_at = NOW() WHERE id = $1::uuid`, nil, orgID, migrated); err != nil {
				return err
			}
			log.Printf("migrated logo for org %s", orgID)
		}
	}
	return rows.Err()
}

func migrateFile(ctx context.Context, store storage.Store, root, location, namespace, owner string, maxSize int64, extensions map[string]bool) (string, error) {
	location = strings.TrimSpace(location)
	if location == "" || strings.HasPrefix(location, "r2://") || strings.HasPrefix(location, "http://") || strings.HasPrefix(location, "https://") {
		return location, nil
	}
	clean := filepath.Clean(filepath.FromSlash(location))
	if filepath.IsAbs(clean) || strings.Contains(clean, "..") || !strings.HasPrefix(filepath.ToSlash(clean), "uploads/") {
		return "", fmt.Errorf("refusing unsafe legacy path %q", location)
	}
	path := filepath.Join(root, clean)
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if err := storage.ValidateUpload("application/octet-stream", path, info.Size(), maxSize, extensions); err != nil {
		return "", err
	}
	head := make([]byte, 512)
	n, _ := io.ReadFull(file, head)
	contentType := http.DetectContentType(head[:n])
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	key, err := storage.BuildKey(namespace, owner+"-"+uuid.NewString(), filepath.Base(path), extensions)
	if err != nil {
		return "", err
	}
	if _, err := store.Put(ctx, key, file, contentType, info.Size()); err != nil {
		return "", err
	}
	object, err := store.Get(ctx, key)
	if err != nil {
		return "", fmt.Errorf("verify migrated object: %w", err)
	}
	_, copyErr := io.Copy(io.Discard, object.Body)
	_ = object.Body.Close()
	if copyErr != nil {
		return "", fmt.Errorf("read migrated object: %w", copyErr)
	}
	return "r2://" + key, nil
}
