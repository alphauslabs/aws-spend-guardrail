# Verify a release

How a reviewer confirms that the guard they deploy was built, unmodified, from this repository's public source. You need no GitHub account and no key from us. Every command uses public tools.

Pick the release you're reviewing, `v0.1.0` in the examples, and download its assets from the release page into one folder: `SHA256SUMS`, `SHA256SUMS.sigstore.json`, `template.yaml`, `guard-v0.1.0-linux-arm64.zip`, and the `guardrail` CLI for your platform if you'll use it.

## 1. Check the signature on SHA256SUMS

Install [cosign](https://docs.sigstore.dev/cosign/system_config/installation/) (v2 or later), then:

```
REPO=https://github.com/alphauslabs/aws-spend-guardrail
WORKFLOW=$REPO/.github/workflows/release.yml
TAG=v0.1.0
cosign verify-blob SHA256SUMS \
  --bundle SHA256SUMS.sigstore.json \
  --certificate-identity "$WORKFLOW@refs/tags/$TAG" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

`Verified OK` means `SHA256SUMS` was signed by this repository's release workflow, for that tag, and hasn't changed since. Anything else means stop.

## 2. Check the files against SHA256SUMS

```
sha256sum --check --ignore-missing SHA256SUMS
```

On macOS, use `shasum -a 256 --check --ignore-missing SHA256SUMS`. Every file you downloaded must say `OK`.

## 3. Rebuild from source and compare (optional, strongest)

The function package and CLI binaries are reproducible: building the same tag gives the same bytes. With `git`, `make`, and Go installed:

```
git clone https://github.com/alphauslabs/aws-spend-guardrail
cd aws-spend-guardrail
git checkout v0.1.0
make release VERSION=v0.1.0
diff <(grep -v sbom SHA256SUMS) <(grep -v sbom dist/SHA256SUMS)
```

Run it from the folder holding the downloaded `SHA256SUMS`, or give its path. No output from `diff` means your build matches the release. The Makefile pins the Go version, so Go downloads it if yours differs. The SBOM isn't part of this comparison: it records when it was generated.

You can then deploy your own `dist/guard-v0.1.0-linux-arm64.zip` instead of ours.

## 4. Compare the deployed function

Each release's notes give the package's SHA-256 in the base64 form AWS Lambda reports. After deploying, with credentials for the payer account:

```
aws lambda get-function --region us-east-1 \
  --function-name guardrail-<msp id> \
  --query Configuration.CodeSha256 --output text
```

It must equal the base64 hash in the release notes. In the console: **Lambda → Functions → guardrail-<msp id> → Code → Code properties → SHA256 hash**. To compute it yourself from a zip: `openssl dgst -sha256 -binary guard-v0.1.0-linux-arm64.zip | base64`.

## 5. Check the record in Jennah

The guard reports its release version and code hash in every `heartbeat` and `config_applied` event. In the payer org's inbox in Jennah, the latest heartbeat's `detail` shows `guard_version` and `code_sha256`. They must match the release and step 4. A `code_sha256` that matches no release means the running function was changed outside a release. `unknown` means the guard couldn't read its own hash; read it from Lambda as in step 4.

## What to read before upgrading

[CHANGELOG.md](../CHANGELOG.md) has an entry for every release. Its **Security-relevant** section lists changes to the freeze policies, IAM permissions, the stack parameters that control approvals, and outbound calls. Review those, and the diff between the two tags, before deploying a new version.
