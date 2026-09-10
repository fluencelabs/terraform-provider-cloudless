package client_test

import (
	"context"
	"testing"

	"github.com/cloudless/terraform-provider-cloudless/internal/client"
	"github.com/cloudless/terraform-provider-cloudless/internal/client/mock"
)

// Attaching a public IP flags the VM restart_required; the attached IP does not
// route until the VM is restarted. RestartVM must clear that flag.
func TestRestartVM_ClearsRestartRequired(t *testing.T) {
	srv := mock.New()
	defer srv.Close()
	c := client.New(srv.URL, "k")
	ctx := context.Background()

	storageID := "33333333-3333-3333-3333-333333333333"
	vm, err := c.CreateVM(ctx, client.CreateVMRequest{
		ClusterID:       "11111111-1111-1111-1111-111111111111",
		Name:            "app",
		ConfigurationID: "22222222-2222-2222-2222-222222222222",
		BootDisk:        client.VMBootDisk{StorageID: &storageID},
	})
	if err != nil {
		t.Fatalf("create vm: %v", err)
	}

	if err = c.AddVMPublicIP(ctx, vm.ID, "44444444-4444-4444-4444-444444444444"); err != nil {
		t.Fatalf("add public ip: %v", err)
	}
	got, err := c.GetVM(ctx, vm.ID)
	if err != nil {
		t.Fatalf("get vm: %v", err)
	}
	if !got.RestartRequired {
		t.Fatal("attaching a public IP should set restart_required")
	}

	out, err := c.RestartVM(ctx, vm.ID)
	if err != nil {
		t.Fatalf("restart vm: %v", err)
	}
	if out.RestartRequired {
		t.Fatal("restart should clear restart_required, got true")
	}
}
