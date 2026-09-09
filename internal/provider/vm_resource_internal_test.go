package provider

import (
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// TestBootDiskToAPI_WireShapes pins the two untagged variants of the /v3
// boot-disk body: an existing storage is a bare JSON string, an inline create
// is an object with volumeGb + imageId (+ name). The server matches by shape,
// so a wrapped or mis-keyed body fails with "did not match any variant".
func TestBootDiskToAPI_WireShapes(t *testing.T) {
	const storageID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	const imageID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"

	existing, err := bootDiskToAPI(&vmBootDiskModel{StorageID: types.StringValue(storageID)})
	if err != nil {
		t.Fatalf("existing: %v", err)
	}
	if got, _ := json.Marshal(existing); string(got) != `"`+storageID+`"` {
		t.Errorf("existing boot disk marshaled as %s, want bare string", got)
	}

	inline, err := bootDiskToAPI(&vmBootDiskModel{
		StorageID: types.StringNull(),
		Name:      types.StringValue("boot"),
		VolumeGb:  types.Int64Value(40),
		ImageID:   types.StringValue(imageID),
	})
	if err != nil {
		t.Fatalf("inline: %v", err)
	}
	got, _ := json.Marshal(inline)
	if want := `{"volumeGb":40,"imageId":"` + imageID + `","name":"boot"}`; string(got) != want {
		t.Errorf("inline boot disk marshaled as %s, want %s", got, want)
	}

	if _, rerr := bootDiskToAPI(&vmBootDiskModel{VolumeGb: types.Int64Value(40)}); rerr == nil {
		t.Error("inline boot disk without image_id should be rejected")
	}
	if _, nerr := bootDiskToAPI(nil); nerr == nil {
		t.Error("missing boot_disk block should be rejected")
	}
}
