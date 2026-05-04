resource "cloudless_storage" "data" {
  cluster_id   = data.cloudless_cluster.main.id
  name         = "data"
  storage_type = "NVME"
  volume_gb    = 200
  replicated   = false
}
