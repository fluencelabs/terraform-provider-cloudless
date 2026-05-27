# Apply pending network changes (public-IP attach, security-group bind) with a
# single restart, only if the VM is flagged restart_required. Place it after the
# attachments so it runs last and coalesces them into one reboot.
resource "cloudless_vm_restart" "main" {
  vm_id = cloudless_vm.app.id

  # Re-evaluate (and replace this resource) whenever an attachment changes, so a
  # new attach is followed by a fresh restart_required check.
  triggers = {
    public_ip      = cloudless_vm_public_ip_attachment.edge.id
    security_group = cloudless_security_group_attachment.edge.id
  }

  depends_on = [
    cloudless_vm_public_ip_attachment.edge,
    cloudless_security_group_attachment.edge,
  ]
}
