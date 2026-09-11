# Name a configuration by what it is, not by its position in a list.
data "cloudless_vm_configuration" "small" {
  slug = "cpu-regular-2vcpu-4gb"
}

# Or by shape — as long as exactly one matches; otherwise the read fails and
# names the candidates.
data "cloudless_vm_configuration" "by_shape" {
  vcpu   = 4
  ram_gb = 8
}
