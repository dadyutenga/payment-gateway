package storage

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type fakeClient struct {
	putKey string
	getKey string
	delKey string
}

func (f *fakeClient) PutObject(_ context.Context, input *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	f.putKey = aws.ToString(input.Key)
	return &s3.PutObjectOutput{ETag: aws.String("etag")}, nil
}

func (f *fakeClient) GetObject(_ context.Context, input *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	f.getKey = aws.ToString(input.Key)
	return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader([]byte("data"))), ContentType: aws.String("image/png"), ContentLength: aws.Int64(4)}, nil
}

func (f *fakeClient) DeleteObject(_ context.Context, input *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	f.delKey = aws.ToString(input.Key)
	return &s3.DeleteObjectOutput{}, nil
}

type fakePresigner struct{}

func (fakePresigner) PresignGetObject(_ context.Context, _ *s3.GetObjectInput, _ ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error) {
	return &v4.PresignedHTTPRequest{URL: "https://example.test/signed"}, nil
}

func TestBuildKeyAndUploadValidation(t *testing.T) {
	allowed := map[string]bool{".png": true, ".pdf": true}
	if _, err := BuildKey("public/branding", "org/../one", `..\logo.png`, allowed); err == nil {
		t.Fatal("expected path-like owner ID rejection")
	}
	key, err := BuildKey("public/branding", "org-one", `..\logo.png`, allowed)
	if err != nil || key != "public/branding/org-one/logo.png" {
		t.Fatalf("unexpected key %q, err=%v", key, err)
	}
	if err := ValidateUpload("image/png", "logo.png", 10, 9, allowed); err == nil {
		t.Fatal("expected size validation failure")
	}
	if err := ValidateUpload("image/svg+xml", "logo.svg", 10, 100, allowed); err == nil {
		t.Fatal("expected extension validation failure")
	}
}

func TestR2StoreOperationsAndPresign(t *testing.T) {
	client := &fakeClient{}
	store, err := NewWithClients("docs89", client, fakePresigner{})
	if err != nil {
		t.Fatal(err)
	}
	info, err := store.Put(context.Background(), "private/one/doc.pdf", bytes.NewReader([]byte("data")), "application/pdf", 4)
	if err != nil || info.ETag != "etag" || client.putKey != "private/one/doc.pdf" {
		t.Fatalf("put failed: info=%+v err=%v", info, err)
	}
	object, err := store.Get(context.Background(), "private/one/doc.pdf")
	if err != nil || client.getKey != "private/one/doc.pdf" {
		t.Fatalf("get failed: object=%+v err=%v", object.Info, err)
	}
	_ = object.Body.Close()
	if err := store.Delete(context.Background(), "private/one/doc.pdf"); err != nil || client.delKey != "private/one/doc.pdf" {
		t.Fatalf("delete failed: err=%v", err)
	}
	if url, err := store.PresignGet(context.Background(), "public/one/logo.png", 300); err != nil || url != "https://example.test/signed" {
		t.Fatalf("presign failed: url=%q err=%v", url, err)
	}
	if _, err := store.PresignGet(context.Background(), "key", 901); err == nil {
		t.Fatal("expected excessive TTL rejection")
	}
}
