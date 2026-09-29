# A VM needs no VPC or subnet of its own: every cluster already has a default
# subnet, and this is how to point at it without hard-coding an id.
data "cloudless_clusters" "all" {}

data "cloudless_subnets" "all" {
  cluster_ids = [data.cloudless_clusters.all.clusters[0].id]
}

locals {
  default_subnet_id = one([for s in data.cloudless_subnets.all.subnets : s.id if s.is_default])
}

resource "cloudless_vm" "app" {
  cluster_id       = data.cloudless_clusters.all.clusters[0].id
  name             = "app"
  configuration_id = "cpu-2-ram-4gb-storage-25gb"

  boot_disk {
    volume_gb = 40
    image_id  = "ubuntu-24-04-x64"
  }

  network_interface {
    type      = "private"
    subnet_id = local.default_subnet_id
  }
}

# Leaving the network_interface blocks out entirely lands the VM on that same
# default subnet — the data source is only needed to name it explicitly.
