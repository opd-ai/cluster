# Tuning Guide

Practical tuning advice for every subsystem in the AI cluster.

---

## 1. When Per-Repo LoRAs Help

LoRA (Low-Rank Adaptation) fine-tunes are worth the cost when:

- The base model's answers about your codebase/docs are frequently wrong or
  overly generic.
- You have ≥ 500 high-quality (prompt, completion) pairs from your corpus.
- Latency is acceptable for the slower fine-tuned model.

**Not** worth it when:
- RAG retrieval alone achieves > 80% answer accuracy on your eval set.
- Your corpus changes frequently (LoRA re-train lag would hurt freshness).
- VRAM is the bottleneck — a LoRA adapter adds ~50–200 MB on top of the
  base model.

### Recommended LoRA parameters

| Base model size | Rank (r) | Alpha | Epochs | Batch size |
|----------------|----------|-------|--------|------------|
| 7B | 8 | 16 | 3 | 4 |
| 13B | 16 | 32 | 3 | 2 |
| 70B | 32 | 64 | 2 | 1 |

Start with `r=8, alpha=16` and increase rank only if training loss plateaus
above 0.1 after epoch 2.

---

## 2. Evaluating LoRA Regressions

After training, run the regression eval before deploying:

```bash
# From cmd/pipeline (or your eval script)
make eval MODEL=llama3.2:13b-lora DATASET=eval/golden.jsonl
```
<!-- REVIEW: `make eval` is not a defined Makefile target and the MODEL/DATASET
flags do not match cmd/eval-harness (which uses -gateway, -namespace, -repo,
-datasets, -out, -threshold). Confirm the intended invocation. -->

Accept the LoRA if and only if:
- Perplexity ≤ base model + 5%
- Task accuracy ≥ base model on held-out eval set
- Latency increase ≤ 10% (TTFT + throughput)

---

## 3. VRAM Sizing

### LLM inference (Ollama, 4-bit quantisation)

| Model | VRAM (4-bit) | Recommended GPU |
|-------|-------------|-----------------|
| 7B | ~5 GB | RTX 3060 (12 GB) |
| 13B | ~9 GB | RTX 3090 (24 GB) |
| 70B | ~40 GB | 2× A100 80 GB |
| 3B (draft for speculative) | ~2 GB | any |

On Apple Silicon, unified memory serves as VRAM:
- M2 Pro 32 GB: comfortable for 13B at 4-bit + embeddings concurrently
- M2 Ultra 192 GB: runs 70B at 4-bit with headroom for image gen

### Image generation

| Checkpoint | VRAM |
|-----------|------|
| SDXL base (fp16) | 8 GB |
| SDXL base (int8) | 5 GB |
| Flux.1-dev (fp16) | 24 GB |
| Flux.1-schnell (fp8) | 12 GB |

### Video generation (Wan2.1, CogVideoX)

| Model | VRAM |
|-------|------|
| CogVideoX-5B (fp16) | 24 GB |
| Wan2.1-T2V-1.3B | 12 GB |
| Wan2.1-T2V-14B | 40 GB |

---

## 4. Picking Schnell vs Dev (Flux)

| Use case | Recommendation |
|----------|---------------|
| Real-time / interactive | `flux.1-schnell` (4 steps, ~2 s on RTX 4090) |
| High-quality output | `flux.1-dev` (50 steps, ~20 s on RTX 4090) |
| Fine-tuning target | `flux.1-dev` (more signal per step) |

---

## 5. Draft Model Selection for Speculative Decoding

Speculative decoding requires a draft model whose vocabulary matches the
target model exactly. Recommended pairs:

| Target model | Draft model | Speedup |
|-------------|------------|---------|
| Llama 3.3 70B | Llama 3.2 3B | 2–3× |
| Qwen2.5 72B | Qwen2.5 1.5B | 2–3× |
| Mistral 7B | Mistral 3B | 1.5–2× |

Configure in the inventory:

```yaml
speculative_decode:
  target: llama3.3:70b
  draft: llama3.2:3b
  draft_tokens: 5
```

---

## 6. RAG Chunking Strategies

| Strategy | Best for | Chunk size | Overlap |
|----------|----------|-----------|---------|
| Fixed-size | Code, logs | 512 tokens | 64 |
| Sentence | Prose docs | 3–5 sentences | 1 sentence |
| Paragraph | Long-form articles | 1 paragraph | 0 |
| Recursive | Mixed content | 256–1024 tokens | 20% |

Default in `cmd/rag`: recursive, 512 tokens, 64-token overlap.

Increase chunk size if:
- Retrieved chunks are consistently missing the answer context.
- Questions require multi-paragraph reasoning.

Decrease chunk size if:
- Qdrant collection is consuming too much memory.
- Retrieval precision is low (top-k returns irrelevant chunks).

---

## 7. WASM Binary Size vs Feature Tradeoffs

The WASM binary size directly affects load time (no streaming compile on all
browsers; Safari requires the whole binary before execution).

Current size targets:

| Budget | Effect |
|--------|--------|
| < 5 MB | Loads in < 2 s on a 20 Mbps connection |
| 5–15 MB | Acceptable; show a loading spinner |
| > 20 MB | Unacceptable for interactive use; profile and trim |

To check current size:

```bash
ls -lh dist/js-wasm/console.wasm
```

To reduce size:
- Use `-ldflags="-s -w"` (strip debug symbols)
- Remove unused Ebitengine sub-packages via `go build -tags ebitenginedebug=false`
- Avoid embedding large assets — serve them from the console HTTP server
  instead
- Run `wasm-opt -O2` from `binaryen` tools (reduces size by ~15–20%):
  ```bash
  wasm-opt -O2 -o dist/js-wasm/console.wasm dist/js-wasm/console.wasm
  ```
