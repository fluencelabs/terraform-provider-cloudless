resource "cloudless_public_ip" "edge" {
  cluster_id   = data.cloudless_cluster.main.id
  name         = "edge"
  address_type = "V4"
}

resource "cloudless_vm_public_ip_attachment" "edge" {
  vm_id        = cloudless_vm.app.id
  public_ip_id = cloudless_public_ip.edge.id
}
