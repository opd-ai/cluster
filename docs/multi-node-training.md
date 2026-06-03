# Multi-Node Training

For namespaces whose merged model exceeds a single GPU's VRAM, the pipeline
supports distributed training via **DeepSpeed ZeRO-3** or **PyTorch FSDP**,
gang-scheduled on the cluster's trainer-tainted nodes.

## Strategy

| Scenario | Tool | Scheduler |
|----------|------|-----------|
| Single-GPU (< node VRAM) | Unsloth 4-bit LoRA | k3s single Job |
| Multi-GPU on one node | DeepSpeed ZeRO-1/2 | k3s single Job (multi-process) |
| Multi-node GPU | DeepSpeed ZeRO-3 or FSDP | Volcano `JobSpec` or Kubeflow `PyTorchJob` |

## Requirements

- All trainer nodes reachable on the Tailscale mesh (MTU ≥ 1500 for NCCL).
- NCCL environment variables set in the Pod spec:

```yaml
env:
  - name: NCCL_SOCKET_IFNAME
    value: tailscale0
  - name: NCCL_IB_DISABLE
    value: "1"   # No InfiniBand; use TCP/tailnet
  - name: NCCL_DEBUG
    value: WARN
```

- Bandwidth floor: **≥ 1 Gbit/s** between trainer nodes recommended; ZeRO-3
  communication overhead is proportional to parameter count × number of nodes.

## DeepSpeed ZeRO-3 Configuration

The `python/train.py` script detects multi-node mode when the `WORLD_SIZE`
environment variable is > 1 and switches to DeepSpeed automatically.
<!-- REVIEW: python/train.py does not currently reference WORLD_SIZE or DeepSpeed
(it is a single-GPU LoRA driver using argparse with --mode/--namespace/--repo).
This multi-node/DeepSpeed auto-switching behavior appears unimplemented; confirm
whether this document is forward-looking or needs updating to match the code. -->

Sample `configs/deepspeed/zero3.json`:

```json
{
  "zero_optimization": {
    "stage": 3,
    "offload_optimizer": {"device": "cpu"},
    "offload_param":     {"device": "cpu"},
    "overlap_comm": true,
    "contiguous_gradients": true,
    "sub_group_size": 1e9,
    "reduce_bucket_size": "auto",
    "stage3_prefetch_bucket_size": "auto",
    "stage3_param_persistence_threshold": "auto",
    "stage3_max_live_parameters": 1e9,
    "stage3_max_reuse_distance": 1e9,
    "stage3_gather_16bit_weights_on_model_save": true
  },
  "bf16": {"enabled": "auto"},
  "gradient_clipping": 1.0,
  "train_micro_batch_size_per_gpu": 2,
  "gradient_accumulation_steps": 4
}
```

## Gang Scheduling with Volcano

Install Volcano on the k3s cluster:

```bash
kubectl apply -f https://raw.githubusercontent.com/volcano-sh/volcano/release-1.8/installer/volcano-development.yaml
```

Submit a multi-node training job:

```bash
kubectl apply -f configs/training/volcano-job.yaml
```

Sample `configs/training/volcano-job.yaml`:

```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: cluster-train-multinode
spec:
  minAvailable: 2
  schedulerName: volcano
  plugins:
    pytorch: ["--master=master", "--worker=worker"]
    env: []
    svc: []
  tasks:
    - replicas: 1
      name: master
      template:
        spec:
          tolerations:
            - key: workload
              operator: Equal
              value: trainer
              effect: NoSchedule
          nodeSelector:
            role: trainer
          containers:
            - name: trainer
              image: ghcr.io/opd-ai/cluster-trainer:latest
              command: ["python3", "/app/python/train.py"]
              resources:
                limits:
                  nvidia.com/gpu: "1"
    - replicas: 1
      name: worker
      template:
        spec:
          tolerations:
            - key: workload
              operator: Equal
              value: trainer
              effect: NoSchedule
          nodeSelector:
            role: trainer
          containers:
            - name: trainer
              image: ghcr.io/opd-ai/cluster-trainer:latest
              command: ["python3", "/app/python/train.py"]
              resources:
                limits:
                  nvidia.com/gpu: "1"
```

## Triggering Multi-Node from the Pipeline

Pass `--multi-node` to `cmd/k8s-trainer` (future flag) or set
`multi_node: true` in the namespace hyperparams section of
`configs/namespaces.yaml`.  The orchestrator will generate a Volcano Job
instead of a standard Kubernetes Job when `multi_node` is enabled.
