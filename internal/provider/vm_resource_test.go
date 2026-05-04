package provider_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	tfharness "github.com/cloudless/terraform-provider-cloudless/internal/provider/testing"
)

// captureID stores the resource's ID into *out so a later step can compare.
func captureID(addr string, out *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[addr]
		if !ok {
			return fmt.Errorf("resource %s not found in state", addr)
		}
		*out = rs.Primary.ID
		return nil
	}
}

// requireSameID asserts that the resource's current ID equals *want — i.e.
// that a smart-Update path did not silently fall back to recreate.
func requireSameID(addr string, want *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[addr]
		if !ok {
			return fmt.Errorf("resource %s not found in state", addr)
		}
		if rs.Primary.ID != *want {
			return fmt.Errorf("%s was recreated: %s -> %s", addr, *want, rs.Primary.ID)
		}
		return nil
	}
}

func TestUnitVM_CreateMinimal(t *testing.T) {
	h := tfharness.New()
	defer h.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "cloudless_storage" "boot" {
  cluster_id   = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
  name         = "boot"
  storage_type = "NVME"
  volume_gb    = 40
  replicated   = false
  os_image     = "https://example.com/img.qcow2"
}

resource "cloudless_vm" "app" {
  cluster_id       = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
  name             = "app"
  configuration_id = "cfcfcfcf-cfcf-4cfc-8cfc-cfcfcfcfcfcf"

  boot_disk {
    storage_id = cloudless_storage.boot.id
  }
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_vm.app", "status", "launched"),
					resource.TestCheckResourceAttr("cloudless_vm.app", "network_interface_ids.#", "1"),
				),
			},
		},
	})
}

func TestUnitVM_DataDiskIDsSmartUpdate(t *testing.T) {
	h := tfharness.New()
	defer h.Close()

	var initialVMID string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "cloudless_storage" "boot" {
  cluster_id   = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
  name         = "boot"
  storage_type = "NVME"
  volume_gb    = 40
  replicated   = false
  os_image     = "https://example.com/img.qcow2"
}

resource "cloudless_storage" "data1" {
  cluster_id   = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
  name         = "data1"
  storage_type = "NVME"
  volume_gb    = 100
  replicated   = false
}

resource "cloudless_vm" "app" {
  cluster_id       = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
  name             = "app"
  configuration_id = "cfcfcfcf-cfcf-4cfc-8cfc-cfcfcfcfcfcf"
  boot_disk { storage_id = cloudless_storage.boot.id }
  data_disk_ids    = [cloudless_storage.data1.id]
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_vm.app", "data_disk_ids.#", "1"),
					captureID("cloudless_vm.app", &initialVMID),
				),
			},
			{
				Config: `
resource "cloudless_storage" "boot" {
  cluster_id   = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
  name         = "boot"
  storage_type = "NVME"
  volume_gb    = 40
  replicated   = false
  os_image     = "https://example.com/img.qcow2"
}

resource "cloudless_storage" "data1" {
  cluster_id   = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
  name         = "data1"
  storage_type = "NVME"
  volume_gb    = 100
  replicated   = false
}

resource "cloudless_storage" "data2" {
  cluster_id   = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
  name         = "data2"
  storage_type = "NVME"
  volume_gb    = 100
  replicated   = false
}

resource "cloudless_vm" "app" {
  cluster_id       = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
  name             = "app"
  configuration_id = "cfcfcfcf-cfcf-4cfc-8cfc-cfcfcfcfcfcf"
  boot_disk { storage_id = cloudless_storage.boot.id }
  data_disk_ids    = [cloudless_storage.data1.id, cloudless_storage.data2.id]
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_vm.app", "data_disk_ids.#", "2"),
					requireSameID("cloudless_vm.app", &initialVMID),
				),
			},
			{
				Config: `
resource "cloudless_storage" "boot" {
  cluster_id   = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
  name         = "boot"
  storage_type = "NVME"
  volume_gb    = 40
  replicated   = false
  os_image     = "https://example.com/img.qcow2"
}

resource "cloudless_storage" "data2" {
  cluster_id   = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
  name         = "data2"
  storage_type = "NVME"
  volume_gb    = 100
  replicated   = false
}

resource "cloudless_vm" "app" {
  cluster_id       = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
  name             = "app"
  configuration_id = "cfcfcfcf-cfcf-4cfc-8cfc-cfcfcfcfcfcf"
  boot_disk { storage_id = cloudless_storage.boot.id }
  data_disk_ids    = [cloudless_storage.data2.id]
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_vm.app", "data_disk_ids.#", "1"),
					requireSameID("cloudless_vm.app", &initialVMID),
				),
			},
		},
	})
}

// vmPartialUpdateBaseConfig: VM attached to data1 only.
func vmPartialUpdateBaseConfig() string {
	return `
resource "cloudless_storage" "boot" {
  cluster_id   = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
  name         = "boot"
  storage_type = "NVME"
  volume_gb    = 40
  replicated   = false
  os_image     = "https://example.com/img.qcow2"
}

resource "cloudless_storage" "data1" {
  cluster_id   = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
  name         = "data1"
  storage_type = "NVME"
  volume_gb    = 100
  replicated   = false
}

resource "cloudless_storage" "data2" {
  cluster_id   = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
  name         = "data2"
  storage_type = "NVME"
  volume_gb    = 100
  replicated   = false
}

resource "cloudless_vm" "app" {
  cluster_id       = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
  name             = "app"
  configuration_id = "cfcfcfcf-cfcf-4cfc-8cfc-cfcfcfcfcfcf"
  boot_disk { storage_id = cloudless_storage.boot.id }
  data_disk_ids    = [cloudless_storage.data1.id]
}
`
}

// vmPartialUpdateRotateConfig: same VM, but swap data1 → data2. The Update
// path will run AddVMStorages([data2]) then RemoveVMStorages([data1]), so the
// FailRemoveVMStorages knob isolates the partial-progress failure mode.
func vmPartialUpdateRotateConfig() string {
	return `
resource "cloudless_storage" "boot" {
  cluster_id   = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
  name         = "boot"
  storage_type = "NVME"
  volume_gb    = 40
  replicated   = false
  os_image     = "https://example.com/img.qcow2"
}

resource "cloudless_storage" "data1" {
  cluster_id   = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
  name         = "data1"
  storage_type = "NVME"
  volume_gb    = 100
  replicated   = false
}

resource "cloudless_storage" "data2" {
  cluster_id   = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
  name         = "data2"
  storage_type = "NVME"
  volume_gb    = 100
  replicated   = false
}

resource "cloudless_vm" "app" {
  cluster_id       = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
  name             = "app"
  configuration_id = "cfcfcfcf-cfcf-4cfc-8cfc-cfcfcfcfcfcf"
  boot_disk { storage_id = cloudless_storage.boot.id }
  data_disk_ids    = [cloudless_storage.data2.id]
}
`
}

func TestUnitVM_PartialUpdateRefreshesState(t *testing.T) {
	h := tfharness.New()
	defer h.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: h.Factories,
		Steps: []resource.TestStep{
			{
				Config: vmPartialUpdateBaseConfig(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_vm.app", "data_disk_ids.#", "1"),
				),
			},
			{
				PreConfig:   func() { h.Mock.FailRemoveVMStorages = true },
				Config:      vmPartialUpdateRotateConfig(),
				ExpectError: regexp.MustCompile(`Detach VM storages failed`),
			},
			{
				PreConfig: func() { h.Mock.FailRemoveVMStorages = false },
				Config:    vmPartialUpdateRotateConfig(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudless_vm.app", "data_disk_ids.#", "1"),
				),
			},
		},
	})
}
