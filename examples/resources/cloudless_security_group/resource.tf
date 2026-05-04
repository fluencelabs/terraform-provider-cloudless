resource "cloudless_security_group" "web" {
  cluster_id = data.cloudless_cluster.main.id
  name       = "web"

  ingress_mode = "allow_listed"
  ingress {
    protocol = "tcp"
    ports    = "443"
    cidr     = "0.0.0.0/0"
  }
  ingress {
    protocol = "tcp"
    ports    = "22"
    cidr     = "10.0.0.0/8"
  }

  # egress_mode defaults to allow_all
}
