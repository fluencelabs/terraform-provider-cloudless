package provider

import (
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// TestBootDiskToAPI_WireShapes pins the two variants of the draft boot disk.
// Since 0.14.0 both carry a kind tag — "existing" with a storage id, "new"
// with volumeGb and a tagged image source (+ name).
func TestBootDiskToAPI_WireShapes(t *testing.T) {
	const storageID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	const imageID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"

	existing, err := bootDiskToAPI(&vmBootDiskModel{StorageID: types.StringValue(storageID)})
	if err != nil {
		t.Fatalf("existing: %v", err)
	}
	wantExisting := `{"kind":"existing","storageId":"` + storageID + `"}`
	if got, _ := json.Marshal(existing); string(got) != wantExisting {
		t.Errorf("existing boot disk marshaled as %s, want %s", got, wantExisting)
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
	want := `{"kind":"new","name":"boot","source":{"imageId":"` + imageID + `","type":"catalog"},"volumeGb":40}`
	if string(got) != want {
		t.Errorf("inline boot disk marshaled as %s, want %s", got, want)
	}

	if _, rerr := bootDiskToAPI(&vmBootDiskModel{VolumeGb: types.Int64Value(40)}); rerr == nil {
		t.Error("inline boot disk without image_id should be rejected")
	}
	if _, nerr := bootDiskToAPI(nil); nerr == nil {
		t.Error("missing boot_disk block should be rejected")
	}
}
