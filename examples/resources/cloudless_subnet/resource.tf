resource "cloudless_subnet" "private" {
  vpc_id    = cloudless_vpc.main.id
  name      = "private"
  ipv4_cidr = "10.0.0.0/24"
}
