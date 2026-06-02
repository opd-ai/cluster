# Secrets Management

This document describes how secrets are managed in the AI cluster.

Secrets are **SOPS-encrypted** in git and decrypted at runtime by FluxCD using
an `age` key seeded during bootstrap.  The raw secret values are never stored
in plain text in the repository.

---

## Tooling

| Tool | Purpose |
|------|---------|
| [`age`](https://github.com/FiloSottile/age) | Symmetric encryption key pair |
| [`sops`](https://github.com/getsops/sops) | File-level secret encryption wrapper |
| [Flux `decryptionRef`](https://fluxcd.io/flux/guides/mozilla-sops/) | In-cluster decryption via Kubernetes Secret |

---

## Bootstrap (first-time setup)

During `make bootstrap` / `./bootstrap`, the bootstrap command generates an age
key pair on the control node and stores:

- The **public key** at `cluster/.sops.yaml` (committed to git, safe to share).
- The **private key** as a Kubernetes Secret named `sops-age` in the
  `flux-system` namespace (never leaves the cluster).

```bash
# 1. Install age (if not present on your workstation)
brew install age          # macOS
apt install age           # Debian/Ubuntu

# 2. Generate an age key pair
age-keygen -o age.key
# Public key: age1xxxxxxxx...

# 3. Store the private key as a Kubernetes Secret (Flux reads this)
kubectl create secret generic sops-age \
  --namespace=flux-system \
  --from-file=age.agekey=age.key

# 4. Delete the local copy — the key now lives only in the cluster
rm age.key
```

---

## Encrypting a new secret

```bash
# Encrypt a Kubernetes Secret manifest before committing
sops --encrypt \
  --age "$(grep '^# public key:' age.key | awk '{print $NF}')" \
  --encrypted-regex '^(data|stringData)$' \
  plain-secret.yaml > encrypted-secret.yaml

# Commit the encrypted file
git add encrypted-secret.yaml
git commit -m "secrets: add encrypted gateway api-keys"
```

The `.sops.yaml` file at the repo root declares the default age recipient so
you do not need to specify `--age` on every command:

```yaml
# .sops.yaml
creation_rules:
  - path_regex: cluster/.*\.yaml$
    age: age1xxxxxxxx...   # replace with your actual public key
```

---

## Decrypting for local inspection

```bash
# Requires the private key to be in SOPS_AGE_KEY_FILE or SOPS_AGE_KEY
export SOPS_AGE_KEY_FILE=~/.config/sops/age/keys.txt
sops --decrypt encrypted-secret.yaml
```

---

## Flux integration

Every Kustomization that references encrypted secrets must include a
`decryptionRef`:

```yaml
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: cluster-services
  namespace: flux-system
spec:
  decryption:
    provider: sops
    secretRef:
      name: sops-age
  # ... rest of spec
```

Flux decrypts SOPS-encrypted files in-cluster before applying them.  No
plain-text secrets are written to etcd.

---

## Key rotation

1. Generate a new age key pair (`age-keygen -o new-age.key`).
2. Re-encrypt all secrets with the new public key:
   ```bash
   find cluster/ -name '*.yaml' -exec \
     sops updatekeys --yes {} \;
   ```
3. Update `.sops.yaml` to replace the old public key with the new one.
4. Store the new private key as the `sops-age` Kubernetes Secret (step 3 of
   bootstrap above), overwriting the old one.
5. Delete both old and new key files from your workstation.
6. Commit and push — Flux reconciles automatically.

---

## What is encrypted

| Secret | Kubernetes resource | Encrypted fields |
|--------|---------------------|-----------------|
| MinIO root credentials | `minio-credentials` | `data.root-user`, `data.root-password` |
| Gateway API keys | `gateway-api-keys` | `data.keys` |
| Grafana admin password | `grafana-credentials` | `data.admin-password` |
| age private key | `sops-age` | entire `data` block (but created via kubectl, not SOPS) |

---

## Emergency access

If the cluster is unreachable and you need to read an encrypted secret:

1. Locate the age private key backup (kept offline in your password manager).
2. `SOPS_AGE_KEY=<private-key> sops --decrypt cluster/overlays/production/encrypted-secret.yaml`

---

## References

- [SOPS documentation](https://github.com/getsops/sops)
- [age encryption](https://github.com/FiloSottile/age)
- [Flux SOPS guide](https://fluxcd.io/flux/guides/mozilla-sops/)
