resource "cloudless_security_group_attachment" "web" {
  network_interface_id = cloudless_vm.app.network_interface_ids[0]
  security_group_id    = cloudless_security_group.web.id
}
