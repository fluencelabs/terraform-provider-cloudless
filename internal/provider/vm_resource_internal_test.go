package provider

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// TestBootDiskToAPI_InlineCarriesClusterID guards a regression where the
// inline-create boot disk was sent without clusterId. The real Fluence API's
// VmBootDisk oneOf create variant is a CreateUserStorageRequest, which requires
// clusterId; omitting it produced a 400 "data did not match any variant of
// untagged enum VmBootDisk" at apply time. The mock server accepted the
// malformed body, so the gap was only visible against the real API.
func TestBootDiskToAPI_InlineCarriesClusterID(t *testing.T) {
	const clusterID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	d := &vmBootDiskModel{
		StorageID:   types.StringNull(),
		Name:        types.StringValue("boot"),
		StorageType: types.StringValue("NVME"),
		VolumeGb:    types.Int64Value(40),
		Replicated:  types.BoolValue(false),
		OSImage:     types.StringValue("https://example.com/img.qcow2"),
	}

	bd, err := bootDiskToAPI(d, clusterID)
	if err != nil {
		t.Fatalf("bootDiskToAPI returned error: %v", err)
	}
	if bd.Create == nil {
		t.Fatalf("expected an inline-create boot disk, got %+v", bd)
	}
	if bd.Create.ClusterID != clusterID {
		t.Errorf("inline boot disk ClusterID = %q, want %q", bd.Create.ClusterID, clusterID)
	}

	// The clusterId must survive marshaling so it reaches the API in the
	// oneOf create variant.
	out, err := json.Marshal(bd)
	if err != nil {
		t.Fatalf("marshal boot disk: %v", err)
	}
	if want := `"clusterId":"` + clusterID + `"`; !strings.Contains(string(out), want) {
		t.Errorf("marshaled boot disk missing %s\n got: %s", want, out)
	}
}
