package mock_test

import (
	"context"
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
