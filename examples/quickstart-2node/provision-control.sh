#!/bin/sh
# provision-control.sh — bootstraps the control node
# Called by Vagrant with CONTROL_IP and WORKER_IP env vars.
set -eu

# Install Go
GO_VERSION="1.25.0"
wget -q "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" -O /tmp/go.tar.gz
tar -C /usr/local -xzf /tmp/go.tar.gz
export PATH=$PATH:/usr/local/go/bin

# Install Ollama
curl -fsSL https://ollama.ai/install.sh | sh
systemctl enable --now ollama

# Pull a tiny model for quickstart (< 2 GB)
ollama pull qwen2.5:1.5b

# Clone the cluster repo if not already mounted
if [ ! -d /home/vagrant/cluster ]; then
  git clone https://github.com/opd-ai/cluster.git /home/vagrant/cluster
fi
cd /home/vagrant/cluster

# Write minimal inventory for 2-node setup
cat > cluster/inventory.yaml <<EOF
nodes:
  - hostname: control
    address: ${CONTROL_IP}
    ssh_user: vagrant
    arch: amd64
    os: linux
    role: control
    models:
      - name: qwen2.5:1.5b

  - hostname: worker1
    address: ${WORKER_IP}
    ssh_user: vagrant
    arch: amd64
    os: linux
    role: worker
    models:
      - name: qwen2.5:1.5b
EOF

# Bootstrap k3s control plane
/usr/local/go/bin/go run ./cmd/cluster-bootstrap --up --inventory cluster/inventory.yaml \
  || k3s server --bind-address "${CONTROL_IP}" --flannel-iface eth1 &
sleep 10

# Copy kubeconfig
mkdir -p ~/.kube
cp /etc/rancher/k3s/k3s.yaml ~/.kube/config
chown vagrant:vagrant ~/.kube/config
