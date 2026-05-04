resource "cloudless_vpc" "main" {
  cluster_id = data.cloudless_cluster.main.id
  name       = "main"
}
