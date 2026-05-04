resource "cloudless_storage" "boot" {
  cluster_id   = data.cloudless_cluster.main.id
  name         = "app-boot"
  storage_type = "NVME"
  volume_gb    = 40
  replicated   = false
  os_image     = data.cloudless_default_images.all.images[0].download_url
}

resource "cloudless_vm" "app" {
  cluster_id       = data.cloudless_cluster.main.id
  name             = "app"
  configuration_id = data.cloudless_vm_configurations.all.configurations[0].id
  ssh_key_ids      = [cloudless_ssh_key.me.id]
  boot_disk { storage_id = cloudless_storage.boot.id }
}
