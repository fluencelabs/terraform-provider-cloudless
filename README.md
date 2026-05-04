# terraform-provider-cloudless

A Terraform provider for the [Fluence](https://fluence.dev) decentralized
compute marketplace.

> Status: feature-complete. Validators, generated docs, CI, and a
> goreleaser pipeline are all in place. The first release tag is a
> follow-up once the registry namespace is decided.

## Resources

| Resource | Description |
| --- | --- |
| `cloudless_ssh_key` | SSH public key reusable across VMs. |
| `cloudless_vpc` | A VPC on a chosen cluster. |
| `cloudless_subnet` | A subnet inside a VPC. `cluster_id` derived from VPC if unset. |
| `cloudless_security_group` | Firewall rules. `ingress_mode` / `egress_mode` enum: `allow_all` (default) / `allow_listed` / `deny_all`. |
| `cloudless_storage` | Block storage volume. `volume_gb` is in-place resizable via PATCH. |
| `cloudless_public_ip` | Static IPv4 address. |
| `cloudless_vm` | A VM. Boot disk inline or referenced by ID. `data_disk_ids` smart-Updates without VM recreation. |
| `cloudless_vm_public_ip_attachment` | Bind a public IP to a VM. |
| `cloudless_security_group_attachment` | Bind a security group to a VM's network interface. |

## Data sources

| Data source | Description |
| --- | --- |
| `cloudless_cluster` | Look up exactly one cluster by `region` / `city_code` / `name` / `id`. Errors on ambiguity. |
| `cloudless_clusters` | List clusters with optional `regions` / `city_codes` / `names` AND-composed filters. |
| `cloudless_vm_configurations` | All VM presets (CPU/RAM). |
| `cloudless_default_images` | Curated default OS images. |

## Provider configuration

```hcl
provider "cloudless" {
  api_key  = var.fluence_api_key  # or set FLUENCE_API_KEY
  endpoint = "https://api.fluence.dev"  # optional override
}
```

## Getting started

```bash
export FLUENCE_API_KEY=...
cd examples
terraform init && terraform apply
```

`examples/main.tf` provisions an SSH key, a VPC + subnet, a `cloudless_storage`
boot disk, and a small VM that references the storage by ID.

Per-resource and per-data-source examples live in `examples/resources/` and
`examples/data-sources/`.

## Local development

This repo ships a Nix flake with all tooling:

```bash
nix develop
go build ./...
go test ./...
```

Tools available in the dev shell: `terraform`, `go`, `tfplugindocs`,
`goreleaser`, `gnupg`.

To use the freshly-built provider against a Terraform config without
publishing:

```hcl
# ~/.terraformrc
provider_installation {
  dev_overrides {
    "registry.terraform.io/cloudless/cloudless" = "/path/to/$GOPATH/bin"
  }
  direct {}
}
```

```bash
go install ./...
terraform plan
```

## Running acceptance tests

Acceptance tests hit the real Fluence API. They are gated on `TF_ACC=1` +
`FLUENCE_API_KEY`. They are NOT run in CI.

```bash
TF_ACC=1 FLUENCE_API_KEY=... go test ./... -run TestAcc -timeout 60m
```

Each acc test names resources with a `tf-acc-<random>-` prefix. The
framework destroys resources at the end of every TestStep, but if a test
panics or the process is killed, manually clean up with the API.

## Documentation

Docs are generated from schema descriptions via `tfplugindocs`. Regenerate
with `make docs`. Generated `docs/` is committed; the registry serves them
at publish time.

## Releasing (out of scope for now)

`goreleaser release --clean` builds signed multi-arch artifacts. Requires
`GPG_FINGERPRINT` env. The first release tag is a follow-up that needs the
registry namespace and a real GPG key.
