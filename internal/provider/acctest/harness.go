// Package acctest provides a setup helper for acceptance tests that hit the
// real Fluence API. Tests skip unless TF_ACC=1 and FLUENCE_API_KEY are set.
package acctest

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/cloudless/terraform-provider-cloudless/internal/client"
	"github.com/cloudless/terraform-provider-cloudless/internal/provider"
)

// Setup ensures TF_ACC=1 and FLUENCE_API_KEY are set, returning provider
// factories pointed at the real API. Calls t.Skip() with a clear reason
// otherwise.
func Setup(t *testing.T) map[string]func() (tfprotov6.ProviderServer, error) {
	t.Helper()
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("set TF_ACC=1 to run acceptance tests")
	}
	if os.Getenv("FLUENCE_API_KEY") == "" {
		t.Skip("set FLUENCE_API_KEY to run acceptance tests")
	}
	return map[string]func() (tfprotov6.ProviderServer, error){
		"cloudless": providerserver.NewProtocol6WithError(provider.New("acc")()),
	}
}

// RealClient returns a *client.Client pointed at the real Fluence API,
// authenticated via FLUENCE_API_KEY (which Setup already verified is set).
func RealClient() *client.Client {
	return client.New(
		os.Getenv("FLUENCE_ENDPOINT"),
		os.Getenv("FLUENCE_API_KEY"),
		client.WithUserAgent("terraform-provider-cloudless/acc"),
	)
}

// DefaultNetwork returns the account's VPC and default subnet for tests that
// need an existing network: API keys cannot create VPCs or subnets (vodopad
// refuses vpc:write / subnet:write on keys), so the topology must pre-exist.
// FLUENCE_ACC_VPC_ID / FLUENCE_ACC_SUBNET_ID override the discovery.
func DefaultNetwork(t *testing.T) (string, string) {
	t.Helper()
	c := RealClient()
	ctx := context.Background()
	if subnetID := os.Getenv("FLUENCE_ACC_SUBNET_ID"); subnetID != "" {
		if vpcID := os.Getenv("FLUENCE_ACC_VPC_ID"); vpcID != "" {
			return vpcID, subnetID
		}
		sn, err := c.GetSubnet(ctx, subnetID)
		if err != nil {
			t.Fatalf("FLUENCE_ACC_SUBNET_ID %s: %v", subnetID, err)
		}
		return sn.VPCID, sn.ID
	}
	subnets, err := c.ListSubnets(ctx)
	if err != nil {
		t.Fatalf("list subnets: %v", err)
	}
	// Prefer the VPC's default subnet; otherwise the first ready one. A
	// pinned FLUENCE_ACC_VPC_ID restricts the choice to that VPC.
	pinnedVPC := os.Getenv("FLUENCE_ACC_VPC_ID")
	var pick *client.Subnet
	for i := range subnets {
		sn := &subnets[i]
		if sn.Status != "ready" || (pinnedVPC != "" && sn.VPCID != pinnedVPC) {
			continue
		}
		if sn.IsDefault {
			pick = sn
			break
		}
		if pick == nil {
			pick = sn
		}
	}
	if pick == nil {
		t.Skip("no ready subnet on the account; create a VPC and subnet in pult or set FLUENCE_ACC_SUBNET_ID")
		return "", "" // unreachable: Skip stops the test
	}
	return pick.VPCID, pick.ID
}

// probeNameSuffixMod keeps the probe VPC's name inside the 25-character limit.
const probeNameSuffixMod = 1_000_000_000

// SkipUnlessVPCWrite skips the test when the key cannot create VPCs. The
// probe VPC is deleted again when the key turns out to be allowed.
func SkipUnlessVPCWrite(t *testing.T, clusterID string) {
	t.Helper()
	// The probe creates a real VPC and deletes it again: body validation runs
	// before the permission check on this route, so a deliberately invalid
	// body answers 400 whether or not the key may write, and cannot tell the
	// two apart (observed on stage 2026-09-11).
	ctx := context.Background()
	name := fmt.Sprintf("tf-acc-probe-%d", time.Now().UnixNano()%probeNameSuffixMod)
	vpc, err := RealClient().CreateVPC(ctx, client.CreateVPCRequest{ClusterID: clusterID, Name: name})
	if client.IsForbidden(err) {
		t.Skip("API key lacks vpc:write (reissue the stage key with vpc:write and subnet:write); " +
			"VPC and subnet resources cannot be exercised")
	}
	if err != nil {
		t.Fatalf("vpc:write probe: %v", err)
	}
	t.Cleanup(func() {
		if derr := RealClient().DeleteVPC(context.Background(), vpc.ID); derr != nil {
			t.Logf("probe VPC %s (%s) left behind: %v", vpc.ID, name, derr)
		}
	})
}

// FirstClusterID returns the first cluster the account can see.
func FirstClusterID(t *testing.T) string {
	t.Helper()
	clusters, err := RealClient().ListClusters(context.Background())
	if err != nil || len(clusters) == 0 {
		t.Fatalf("list clusters: %v (got %d)", err, len(clusters))
	}
	return clusters[0].ID
}
