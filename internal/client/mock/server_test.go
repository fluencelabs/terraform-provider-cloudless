package mock_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/cloudless/terraform-provider-cloudless/internal/client"
	"github.com/cloudless/terraform-provider-cloudless/internal/client/mock"
)

func TestMockServer_404IsTypedAPIError(t *testing.T) {
	srv := mock.New()
	defer srv.Close()

	c := client.New(srv.URL, "test-key")

	_, err := c.GetSSHKey(context.Background(), "00000000-0000-0000-0000-000000000000")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !client.IsNotFound(err) {
		t.Fatalf("expected client.IsNotFound to be true, got %v", err)
	}
}

// The gate must be able to see a missing Idempotency-Key: the contract
// middleware validates bodies only, so the mock is the only thing that can.
func TestMock_CreateVMRequiresIdempotencyKey(t *testing.T) {
	s := mock.New()
	defer s.Close()

	req, err := http.NewRequest(http.MethodPost, s.URL+"/v3/vms", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("create without Idempotency-Key: got %d, want 400", resp.StatusCode)
	}
}
