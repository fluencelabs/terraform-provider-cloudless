# Publishing

This provider publishes to the **Terraform Registry** (registry.terraform.io)
and the **OpenTofu Registry** (search.opentofu.org). Both consume the same
GPG-signed GitHub release produced by `.goreleaser.yml` /
`.github/workflows/release.yml`.

## Edge builds (latest `main`)

Every push to `main` triggers the `edge` workflow (`.github/workflows/edge.yml`),
which builds an **unsigned snapshot** for all platforms and publishes it to a
rolling `edge` GitHub pre-release. These are **not** in any registry — registries
only accept signed, tagged releases — so consume them locally:

**Option A — dev_overrides** (no version pinning, simplest):

```sh
# Download + unzip the asset for your OS/arch from the 'edge' release, e.g.
gh release download edge --pattern '*darwin_arm64*' --output edge.zip
unzip edge.zip -d ~/go/bin/    # extracts terraform-provider-cloudless_v0.0.0-edge-<sha>
```

```hcl
# ~/.terraformrc  (OpenTofu: ~/.tofurc)
provider_installation {
  dev_overrides { "cloudless/cloudless" = "/Users/you/go/bin" }
  direct {}
}
```

Then run `terraform plan` directly (skip `init`).

**Option B — filesystem mirror** (resolves the real `source` address). Lay the
unzipped binary out under:

```
<mirror>/registry.terraform.io/cloudless/cloudless/0.0.0-edge-<sha>/<os>_<arch>/terraform-provider-cloudless_v0.0.0-edge-<sha>
```

and point `provider_installation { filesystem_mirror { path = "<mirror>" } }` at it.

The `edge` tag always points at the latest `main` commit; old artifacts are
overwritten on each push.

## One-time setup

### 1. Generate a GPG signing key

```sh
# Generate a key (choose RSA 4096, no expiry or a long one). Use a real email.
gpg --full-generate-key

# Find the key id (the long hex after rsa4096/).
gpg --list-secret-keys --keyid-format=long

# Export the PRIVATE key (for CI). Keep this secret.
gpg --armor --export-secret-keys <KEY_ID> > private.asc

# Export the PUBLIC key (for the registries).
gpg --armor --export <KEY_ID> > public.asc
```

### 2. Add CI secrets

In the GitHub repo: **Settings → Secrets and variables → Actions → New secret**

| Secret            | Value                                              |
| ----------------- | -------------------------------------------------- |
| `GPG_PRIVATE_KEY` | contents of `private.asc`                          |
| `PASSPHRASE`      | the passphrase you set when generating the key     |

The release workflow imports this key and exports its fingerprint as
`GPG_FINGERPRINT`, which `.goreleaser.yml` uses to sign `SHA256SUMS`.

Delete `private.asc` from disk once the secret is set.

### 3. Register on the Terraform Registry

1. The GitHub repo must be **public** and named `terraform-provider-cloudless`.
2. Sign in at https://registry.terraform.io with GitHub.
3. **Publish → Provider**, authorize, select the repo.
4. Add the **public** key (`public.asc`) to your registry namespace under
   **User Settings → Signing Keys**.

### 4. Register on the OpenTofu Registry

OpenTofu uses a PR-based registry. Open a PR against
https://github.com/opentofu/registry adding a provider entry under your
namespace and the GPG **public** key. See
https://github.com/opentofu/registry#adding-a-provider for the current format.

## Cutting a release

Releases are automated with [release-please](https://github.com/googleapis/release-please)
driven by [Conventional Commits](https://www.conventionalcommits.org/):

- `feat: ...` → minor bump
- `fix: ...` → patch bump
- `feat!: ...` or a `BREAKING CHANGE:` footer → major bump

The flow:

1. Push commits to `main`. The `release` workflow opens (or updates) a
   **release PR** that bumps the version in `.release-please-manifest.json`
   and writes `CHANGELOG.md`.
2. **Merge the release PR.** release-please then creates the `vX.Y.Z` tag and a
   GitHub Release containing the changelog.
3. In the same workflow run, GoReleaser builds all platform binaries, signs the
   checksums, and **appends** the artifacts to that release.
4. Both registries detect the new release automatically (Terraform Registry via
   webhook, OpenTofu on its sync schedule).

> Tags are `vMAJOR.MINOR.PATCH` (semver), produced by release-please. The
> registry will not pick up a release whose checksum signature does not verify
> against your public key.

> **Why not `git tag` manually?** A tag pushed by CI's `GITHUB_TOKEN` does not
> trigger other workflows, so signing/build runs in the *same* workflow right
> after release-please, gated on `release_created`. Merging the release PR is
> the trigger.
