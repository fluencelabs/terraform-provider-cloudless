# Name an image by its slug instead of filtering the whole catalog.
data "cloudless_default_image" "ubuntu" {
  slug = "ubuntu-24-04-x64"
}

output "ssh_user" {
  # The account the image ships with.
  value = data.cloudless_default_image.ubuntu.username
}
