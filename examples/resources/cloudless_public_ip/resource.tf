resource "cloudless_public_ip" "edge" {
  cluster_id   = data.cloudless_cluster.main.id
  name         = "edge"
  address_type = "V4"
}
