package storage

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"unicode"
)

type ObjectInfo struct {
	Key         string
	ContentType string
	Size        int64
	ETag        string
}

type Object struct {
	Info ObjectInfo
	Body io.ReadCloser
}

type Store interface {
	Put(ctx context.Context, key string, body io.Reader, contentType string, size int64) (ObjectInfo, error)
	Get(ctx context.Context, key string) (Object, error)
	Delete(ctx context.Context, key string) error
	PresignGet(ctx context.Context, key string, ttlSeconds int64) (string, error)
}

var ErrInvalidKey = fmt.Errorf("invalid storage object key")

// BuildKey creates a bounded, path-confined key. Namespace and owner IDs are
// supplied by trusted server-side code, never by a client.
func BuildKey(namespace, ownerID, filename string, allowedExtensions map[string]bool) (string, error) {
	namespace = cleanPath(namespace)
	cleanOwnerID := cleanSegment(ownerID)
	if ownerID != cleanOwnerID || strings.ContainsAny(ownerID, "/\\") {
		return "", ErrInvalidKey
	}
	ownerID = cleanOwnerID
	filename = strings.ReplaceAll(strings.TrimSpace(filename), "\\", "/")
	filename = filepath.Base(filename)
	if namespace == "" || ownerID == "" || filename == "." || filename == "" || strings.Contains(filename, "/") {
		return "", ErrInvalidKey
	}
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == "" || !allowedExtensions[ext] {
		return "", fmt.Errorf("%w: extension %q is not allowed", ErrInvalidKey, ext)
	}
	base := cleanSegment(strings.TrimSuffix(filename, filepath.Ext(filename)))
	if base == "" {
		base = "file"
	}
	if len([]rune(base)) > 80 {
		base = string([]rune(base)[:80])
	}
	return namespace + "/" + ownerID + "/" + base + ext, nil
}

func cleanPath(value string) string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == '/' || r == '\\' })
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		if segment := cleanSegment(part); segment != "" {
			cleaned = append(cleaned, segment)
		}
	}
	return strings.Join(cleaned, "/")
}

func cleanSegment(value string) string {
	var builder strings.Builder
	for _, r := range strings.TrimSpace(value) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
			builder.WriteRune(r)
		}
	}
	return strings.Trim(builder.String(), ".-")
}

func ValidateUpload(contentType, filename string, size, maxSize int64, allowed map[string]bool) error {
	if size < 0 || size > maxSize {
		return fmt.Errorf("file exceeds the %d byte limit", maxSize)
	}
	ext := strings.ToLower(filepath.Ext(filename))
	if !allowed[ext] {
		return fmt.Errorf("file type %q is not allowed", contentType)
	}
	if strings.TrimSpace(contentType) == "" {
		return fmt.Errorf("content type is required")
	}
	return nil
}

func validatePresignTTL(ttlSeconds int64) error {
	if ttlSeconds < 1 || ttlSeconds > 15*60 {
		return fmt.Errorf("presigned URL lifetime must be between 1 and 900 seconds")
	}
	return nil
}
