package client_test

import (
	"context"
	"testing"

	"github.com/cloudless/terraform-provider-cloudless/internal/client"
	"github.com/cloudless/terraform-provider-cloudless/internal/client/mock"
)

// Removing an interface from a live VM flags it restart_required; the change
// takes effect on restart. RestartVM must clear that flag.
func TestRestartVM_ClearsRestartRequired(t *testing.T) {
	srv := mock.New()
	defer srv.Close()
	c := client.New(srv.URL, "k")
	ctx := context.Background()

	storageID := "33333333-3333-3333-3333-333333333333"
	vm, err := seedLiveVM(ctx, c, storageID)
	if err != nil {
		t.Fatalf("create vm: %v", err)
	}

	iface, err := c.AddVMInterface(ctx, vm.ID, client.AddInterfaceRequest{
		Public: true, PublicIPID: "44444444-4444-4444-4444-444444444444",
	})
	if err != nil {
		t.Fatalf("add public interface: %v", err)
	}
	// Observed on stage: attaching applies at once; removing an interface is
	// what flags the VM restart_required.
	if err = c.RemoveVMInterface(ctx, vm.ID, iface.ID); err != nil {
		t.Fatalf("remove public interface: %v", err)
	}
	got, err := c.GetVM(ctx, vm.ID)
	if err != nil {
		t.Fatalf("get vm: %v", err)
	}
	if !got.RestartRequired {
		t.Fatal("removing an interface should set restart_required")
	}

	out, err := c.RestartVM(ctx, vm.ID)
	if err != nil {
		t.Fatalf("restart vm: %v", err)
	}
	if out.RestartRequired {
		t.Fatal("restart should clear restart_required, got true")
	}
}

// seedLiveVM provisions a VM through the /v3 draft flow with an existing boot
// disk and returns the live VM.
func seedLiveVM(ctx context.Context, c *client.Client, bootStorageID string) (*client.VM, error) {
	draft, err := c.CreateVMDraft(ctx, client.VMDraftRequest{
		BootDisk: &client.DraftBootDisk{StorageID: bootStorageID},
	})
	if err != nil {
		return nil, err
	}
	return c.ProvisionVM(ctx, draft.ID)
}
