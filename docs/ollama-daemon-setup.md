# Per-Node Ollama Daemon Setup

Each GPU worker node in the cluster runs a local [Ollama](https://ollama.ai)
daemon that serves models over the Ollama HTTP API on port **11434**.  
The gateway (`cmd/gateway`) can discover these daemons via the
**zero-configuration discovery protocol** (UDP multicast on `239.77.0.1:9977`)
when started with `--discovery=true`.

## Zero-Configuration Setup (Recommended)

The easiest way to set up Ollama is through `cmd/node-deploy`:

```bash
# Generate role service unit/plist definitions
make deploy ROLES=chat

# Start node-agent for discovery (broadcasts to gateway and peers)
NODE_AGENT_API_KEY=change-me
go run ./cmd/node-agent --roles chat --address "$(tailscale ip -4)" --api-key "$NODE_AGENT_API_KEY"
```

This will:
1. Generate service definitions from detected hardware budgets.
2. Write systemd units (Linux) or launchd plists (macOS) for requested roles.
3. Start node-agent for automatic discovery only when you run it separately.

When gateway discovery is enabled (`--discovery=true`), the gateway can discover this node and route requests to it.

## Manual Linux Setup — systemd Unit (Legacy)

For manual configuration, deploy the included drop-in to every Ubuntu/Debian and RHEL worker:

```ini
# /etc/systemd/system/ollama.service
[Unit]
Description=Ollama LLM daemon
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/local/bin/ollama serve
Restart=on-failure
RestartSec=5
User=ollama
Group=ollama
Environment="OLLAMA_HOST=0.0.0.0"
Environment="OLLAMA_MODELS=/var/lib/aicluster/hot"
EnvironmentFile=-/etc/ollama/env
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
```

Install and start:

```bash
sudo useradd -r -s /bin/false -d /var/lib/aicluster/hot ollama || true
sudo cp configs/ollama/ollama.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now ollama
```

Verify the daemon is healthy:

```bash
curl http://localhost:11434/api/tags
```

### Environment Overrides (`/etc/ollama/env`)

| Variable | Default | Description |
|----------|---------|-------------|
| `OLLAMA_HOST` | `0.0.0.0` | Bind address (use Tailscale IP to restrict) |
| `OLLAMA_MODELS` | `/var/lib/aicluster/hot` | Model storage path |
| `OLLAMA_NUM_PARALLEL` | `1` | Concurrent request slots per model |
| `OLLAMA_MAX_LOADED_MODELS` | `3` | Max models kept in VRAM |
| `OLLAMA_FLASH_ATTENTION` | `1` | Enable Flash Attention (A100/H100) |
| `CUDA_VISIBLE_DEVICES` | _(all)_ | Restrict to specific GPU indices |

## Manual macOS Setup — launchd Plist (Legacy)

For manual configuration on macOS nodes:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>ai.ollama.daemon</string>
  <key>ProgramArguments</key>
  <array>
    <string>/usr/local/bin/ollama</string>
    <string>serve</string>
  </array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>OLLAMA_HOST</key>
    <string>0.0.0.0</string>
    <key>OLLAMA_MODELS</key>
    <string>/var/lib/aicluster/hot</string>
  </dict>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>/var/log/ollama.log</string>
  <key>StandardErrorPath</key>
  <string>/var/log/ollama.log</string>
</dict>
</plist>
```

Install:

```bash
sudo cp configs/ollama/ai.ollama.daemon.plist /Library/LaunchDaemons/
sudo launchctl load -w /Library/LaunchDaemons/ai.ollama.daemon.plist
```

## Automated Provisioning (Legacy Inventory Path)

`cmd/cluster-bootstrap` installs Ollama automatically on Linux workers via
the `bootstrapUbuntuDebian` and `bootstrapRHEL` paths when using manual inventory.
After bootstrap, run:

```bash
cluster-bootstrap --inventory cluster/inventory.yaml
```

The daemon is started automatically as part of the bootstrap sequence.

**Note:** For zero-conf deployments, use `make deploy ROLES=chat` instead.

## Health Verification

With zero-conf, the gateway automatically discovers and health-checks backends.
The gateway probes `/api/tags` on each backend every 15 seconds (configurable
via `--probe-interval`).  A node is marked unhealthy if the probe fails or
returns a non-200 status.

### Zero-Conf Health Check

```bash
# Check node-agent health endpoint
curl -H "Authorization: ******" http://localhost:9977/api/v1/health | jq

# Check discovered peers
curl -H "Authorization: ******" http://localhost:9977/api/v1/peers | jq
```

### Manual Health Check (Legacy)

```bash
# Manual check using inventory file
for node in $(grep 'address:' cluster/inventory.yaml | awk '{print $2}'); do
  echo -n "$node: "
  curl -s "http://$node:11434/api/tags" | jq '.models | length' 2>/dev/null || echo "UNREACHABLE"
done
```
