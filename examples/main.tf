terraform {
  required_providers {
    cloudless = {
      source = "registry.terraform.io/cloudless/cloudless"
    }
  }
}

provider "cloudless" {
  # api_key is read from FLUENCE_API_KEY when omitted
}

# Browse what's on the marketplace before referencing IDs.
data "cloudless_clusters" "all" {}
data "cloudless_vm_configurations" "all" {}
data "cloudless_default_images" "all" {}

locals {
  cluster_id      = data.cloudless_clusters.all.clusters[0].id
  small_config_id = [for c in data.cloudless_vm_configurations.all.configurations : c.id if c.vcpu == 2][0]
  ubuntu_image    = [for i in data.cloudless_default_images.all.images : i.download_url if i.slug == "ubuntu-24-04-x64"][0]
}

resource "cloudless_ssh_key" "me" {
  name       = "example-key"
  public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIKgJIjnDg1DjqOOxINs78oU3f7PJXIyq9uiNocNVhXNx user@example.com"
}

resource "cloudless_vpc" "default" {
  cluster_id      = local.cluster_id
  name            = "example-vpc"
  enable_external = true
}

resource "cloudless_subnet" "default" {
  vpc_id     = cloudless_vpc.default.id
  cluster_id = local.cluster_id
  name       = "example-subnet"
  ipv4_cidr  = "10.0.0.0/24"
}

resource "cloudless_storage" "boot" {
  cluster_id   = local.cluster_id
  name         = "example-boot"
  storage_type = "NVME"
  volume_gb    = 40
  replicated   = false
  os_image     = local.ubuntu_image
}

resource "cloudless_vm" "example" {
  cluster_id       = local.cluster_id
  name             = "example-vm"
  configuration_id = local.small_config_id
  ssh_key_ids      = [cloudless_ssh_key.me.id]

  boot_disk {
    storage_id = cloudless_storage.boot.id
  }

  depends_on = [cloudless_subnet.default]
}
