# Runbook: Rotate Secrets and API Keys

## Prerequisites

- `kubectl` access to the cluster (`cluster/kubeconfig`)
- `age` CLI installed (for re-encrypting SOPS secrets)
- `sops` CLI installed

---

## 1. Rotate Gateway API Keys

```bash
# Generate new keys (one per line, any opaque string)
NEW_KEY_1=$(openssl rand -hex 32)
NEW_KEY_2=$(openssl rand -hex 32)

# Update the Secret
kubectl -n ai-cluster create secret generic gateway-api-keys \
  --from-literal="keys=${NEW_KEY_1}\n${NEW_KEY_2}" \
  --dry-run=client -o yaml | kubectl apply -f -

# Restart gateway to pick up new keys
kubectl -n ai-cluster rollout restart deployment/gateway
```

---

## 2. Rotate age Encryption Key (SOPS)

```bash
# Generate a new age key pair
age-keygen -o new-age-key.txt
NEW_PUBLIC=$(grep 'public key:' new-age-key.txt | awk '{print $4}')

# Re-encrypt every SOPS-managed secret with the new key
# (add new recipient, then remove old one)
for file in $(find cluster/ -name '*.enc.yaml'); do
  sops --rotate --add-age "${NEW_PUBLIC}" "$file"
done

# Update cluster-wide age secret used by Flux SOPS provider
kubectl -n flux-system create secret generic sops-age \
  --from-file=age.agekey=new-age-key.txt \
  --dry-run=client -o yaml | kubectl apply -f -

# Remove old public key from .sops.yaml and re-encrypt
sed -i '/OLD_PUBLIC_KEY/d' .sops.yaml
```

---

## 3. Rotate SSH Host Keys

```bash
# On each node, regenerate host keys
sudo ssh-keygen -A

# Update known_hosts on all admin machines
ssh-keyscan <node-ip> > ~/.ssh/known_hosts_new
# Merge or replace ~/.ssh/known_hosts as appropriate
```

---

## 4. Rotate MinIO Root Credentials

```bash
# Set new credentials on the MinIO pod
NEW_USER=$(openssl rand -hex 16)
NEW_PASS=$(openssl rand -hex 32)

kubectl -n ai-cluster create secret generic minio-credentials \
  --from-literal=root-user="${NEW_USER}" \
  --from-literal=root-password="${NEW_PASS}" \
  --dry-run=client -o yaml | kubectl apply -f -

kubectl -n ai-cluster rollout restart deployment/minio
```

---

## 5. Rotate TLS Certificates (cert-manager)

Certificates issued by cert-manager auto-renew 15 days before expiry
(`renewBefore: 360h` in `cert-manager.yaml`). To force immediate renewal:

```bash
# Delete the TLS secret — cert-manager will re-issue immediately
kubectl -n ai-cluster delete secret gateway-tls
kubectl -n ai-cluster delete secret rag-mtls
# Repeat for other certs as needed
```
