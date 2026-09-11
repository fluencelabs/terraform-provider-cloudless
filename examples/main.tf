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

# Name what you want; the plural data sources (cloudless_clusters,
# cloudless_vm_configurations, cloudless_default_images) are for browsing the
# catalog when you do not yet know what to ask for.
data "cloudless_cluster" "main" {
  region = "DE"
}

data "cloudless_vm_configuration" "small" {
  vcpu   = 2
  ram_gb = 4
}

data "cloudless_default_image" "ubuntu" {
  slug = "ubuntu-24-04-x64"
}

locals {
  cluster_id      = data.cloudless_cluster.main.id
  small_config_id = data.cloudless_vm_configuration.small.id
  ubuntu_image    = data.cloudless_default_image.ubuntu.download_url
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
