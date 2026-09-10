package provider

import (
	"context"
	"testing"

	"github.com/cloudless/terraform-provider-cloudless/internal/client"
	"github.com/cloudless/terraform-provider-cloudless/internal/client/mock"
)

func newVMWithClient(t *testing.T) (*client.Client, *mock.Server, string) {
	t.Helper()
	srv := mock.New()
	c := client.New(srv.URL, "k")
	storageID := "33333333-3333-3333-3333-333333333333"
	vm, err := c.CreateVM(context.Background(), client.CreateVMRequest{
		ClusterID:       "11111111-1111-1111-1111-111111111111",
		Name:            "app",
		ConfigurationID: "22222222-2222-2222-2222-222222222222",
		BootDisk:        client.VMBootDisk{StorageID: &storageID},
	})
	if err != nil {
		srv.Close()
		t.Fatalf("create vm: %v", err)
	}
	return c, srv, vm.ID
}

// After an attach the VM is flagged restart_required; restartIfFlagged must
// restart it once and report that it did, leaving the flag cleared and the VM
// ready.
func TestRestartIfFlagged_RestartsWhenFlagged(t *testing.T) {
	c, srv, vmID := newVMWithClient(t)
	defer srv.Close()
	ctx := context.Background()

	if err := c.AddVMPublicIP(ctx, vmID, "44444444-4444-4444-4444-444444444444"); err != nil {
		t.Fatalf("add public ip: %v", err)
	}

	restarted, err := restartIfFlagged(ctx, c, vmID)
	if err != nil {
		t.Fatalf("restartIfFlagged: %v", err)
	}
	if !restarted {
		t.Fatal("expected a restart to be issued for a restart_required VM")
	}
	if n := srv.RestartCount(); n != 1 {
		t.Fatalf("want exactly 1 restart, got %d", n)
	}
	got, err := c.GetVM(ctx, vmID)
	if err != nil {
		t.Fatalf("get vm: %v", err)
	}
	if got.RestartRequired {
		t.Fatal("restart_required should be cleared after restart")
	}
}

// A VM that is not flagged restart_required must not be rebooted, and the
// resource must report that no restart happened.
func TestRestartIfFlagged_NoopWhenNotFlagged(t *testing.T) {
	c, srv, vmID := newVMWithClient(t)
	defer srv.Close()

	restarted, err := restartIfFlagged(context.Background(), c, vmID)
	if err != nil {
		t.Fatalf("restartIfFlagged: %v", err)
	}
	if restarted {
		t.Fatal("a VM that is not restart_required must not be restarted")
	}
	if n := srv.RestartCount(); n != 0 {
		t.Fatalf("want 0 restarts for an unflagged VM, got %d", n)
	}
}
