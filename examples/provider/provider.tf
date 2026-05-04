provider "cloudless" {
  api_key  = var.fluence_api_key       # or set FLUENCE_API_KEY
  endpoint = "https://api.fluence.dev" # optional override
}

variable "fluence_api_key" {
  type        = string
  sensitive   = true
  description = "Fluence API key. Can also be set via FLUENCE_API_KEY env var."
}
