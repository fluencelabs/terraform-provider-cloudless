# A VM with its network assembled in one apply: a private default interface
# in a subnet with a security group, plus a public IP the VM creates and owns.
resource "cloudless_vm" "web" {
  cluster_id       = data.cloudless_cluster.main.id
  name             = "web"
  configuration_id = data.cloudless_vm_configuration.small.id
  ssh_key_ids      = [cloudless_ssh_key.me.id]

  boot_disk {
    volume_gb = 40
    image_id  = data.cloudless_default_image.ubuntu.id
  }

  network_interface {
    type              = "private"
    subnet_id         = cloudless_subnet.default.id
    security_group_id = cloudless_security_group.web.id
  }

  network_interface {
    type = "public" # no public_ip_id: the VM creates and owns the address
  }
}

# Boot from a storage volume you manage separately and attach a public IP that
# outlives the VM.
resource "cloudless_storage" "boot" {
  cluster_id   = data.cloudless_cluster.main.id
  name         = "app-boot"
  storage_type = "NVME"
  volume_gb    = 40
  replicated   = false
  os_image     = data.cloudless_default_image.ubuntu.download_url
}

resource "cloudless_public_ip" "app" {
  cluster_id   = data.cloudless_cluster.main.id
  name         = "app"
  address_type = "V4"
}

resource "cloudless_vm" "app" {
  cluster_id       = data.cloudless_cluster.main.id
  name             = "app"
  configuration_id = data.cloudless_vm_configuration.small.id
  ssh_key_ids      = [cloudless_ssh_key.me.id]

  boot_disk { storage_id = cloudless_storage.boot.id }

  network_interface {
    type      = "private"
    subnet_id = cloudless_subnet.default.id
  }

  network_interface {
    type         = "public"
    public_ip_id = cloudless_public_ip.app.id
  }
}
