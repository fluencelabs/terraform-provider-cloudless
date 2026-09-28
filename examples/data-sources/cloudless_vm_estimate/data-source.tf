# What a VM would cost, before it exists. The figure lands in `terraform plan`,
# so the price is visible at review time rather than on the next invoice.
data "cloudless_cluster" "main" { region = "DE" }

data "cloudless_vm_configuration" "small" {
  vcpu   = 2
  ram_gb = 4
}

data "cloudless_default_image" "ubuntu" {
  slug = "ubuntu-24-04-x64"
}

data "cloudless_subnets" "all" {
  cluster_ids = [data.cloudless_cluster.main.id]
}

data "cloudless_vm_estimate" "app" {
  name             = "app"
  cluster_id       = data.cloudless_cluster.main.id
  configuration_id = data.cloudless_vm_configuration.small.id

  boot_disk {
    volume_gb = 40
    image_id  = data.cloudless_default_image.ubuntu.id
  }

  # The address is usually the part that surprises people.
  network_interface {
    type = "public"
  }

  network_interface {
    type      = "private"
    subnet_id = one([for s in data.cloudless_subnets.all.subnets : s.id if s.is_default])
  }
}

output "hourly_cost" {
  value = "${data.cloudless_vm_estimate.app.hourly_total} ${data.cloudless_vm_estimate.app.currency}/h"
}
