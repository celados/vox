---
type: Reference
title: Local speaker identity
description: Runtime boundary, model provenance, cache layout, and validation for named meeting transcripts.
generated: { by: codex, at: 2026-10-16 }
---

# Contract and architecture

Usage: `vox @skill`. Typed contract: `vox @schema .speaker` and [src/schema.ts](../src/schema.ts).

Bun owns the CLI, speaker profile store, candidate selection, matching policy,
ASR cache, and transcript rendering. The embedded Python helper owns audio
excerpts, Kaldi features, and local MLX embedding inference. Its protocol is one
JSON request/response over stdin/stdout; diagnostics use stderr. The helper is
materialized with its lockfile and license under `~/.vox/runtime/<content-hash>`.
`uv` resolves the locked environment there; the shipped Bun bundle has no runtime
checkout dependency. Host prerequisites are Apple Silicon macOS, uv, and ffmpeg.

# Model provenance

Model version: `wespeaker-resnet34-lm-v1`.

The [official WeSpeaker VoxCeleb ResNet34-LM](https://huggingface.co/Wespeaker/wespeaker-voxceleb-resnet34-LM)
ONNX tensor data is pinned in [runtime.py](../python/runtime.py) by revision and
SHA256. No downloaded graph or Python code is executed. First use converts
BatchNorm-fused tensors to MLX safetensors. Subsequent loads verify the local
converted checksum. Network weights and the official `mean_vec` centering are
both preserved; the centered, L2-normalized embedding convention is part of the
model version. Do not mix these profiles with uncentered PyTorch/pyannote vectors.

Weights, source download cache, and attribution stay in `~/.vox/models/`.
Profiles contain embeddings only, not enrollment recordings. Model weights use
CC-BY-4.0; adapted architecture source retains Apache-2.0 attribution.

# Matching policy

Enrollment uses one clean 3–30 second solo clip. Identification selects up to
three non-overlapping 3–10 second excerpts for each anonymous ASR speaker.
Both paths remove near-digital silence lasting at least 300ms before CMN and
temporal pooling, retaining 60ms at speech boundaries. The energy cutoff is the
larger of RMS 1e-5 and 0.001 times peak frame RMS. This conservative filter is not
a speech/noise classifier; clean solo audio is still required. At least 3 seconds
must remain after filtering. Profiles enrolled before this filter should be
re-enrolled from their source audio if they included long pauses.

Cosine
matching requires both the aggregate threshold and candidate margin, and each
excerpt must independently support the selected identity. Unknown or conflicting
excerpts leave the cluster anonymous. Two anonymous clusters may map to one
person, but a merged multi-person cluster must not be forced to one name.

Threshold 0.65 and margin 0.1 are uncalibrated defaults. Similarity scores are not
probabilities. Real meetings, short interjections, recording devices, codecs, and
overlapping speech still require empirical evaluation.

The original `run.json` is the ASR cache. Names are stored in `identified.json`
and rendered to Markdown, preserving original speaker IDs. Export reads this
snapshot. `speaker.identify` recomputes it after profile/config changes without
calling ASR. The raw run remains recoverable if identity fails.

# Validation

- `bun run check`: formatting, TypeScript, and 55 offline tests, including unknown
  rejection, mixed known/unknown cluster rejection, candidate ambiguity, overlap
  subtraction, CLI schema fields, and transcript exports without word timing.
- `uv run --locked --project python python -m unittest discover -s python`: nine
  offline numerical/validation tests; no model download during the suite.
- M1 Pro: actual bundled CLI enrollment from an empty isolated cache completed
  dependency/model download and created two profiles.
- Live Fun-ASR: a synthetic two-voice recording (~22 seconds) produced two
  anonymous speakers; local MLX identified Alice/Bob with cosine ~0.988/~0.984,
  and emitted a timestamped named Markdown transcript. Repeating reused the ASR
  cache. This is synthetic smoke evidence, not a real-meeting accuracy result.
- Bundled `speaker.identify` worked with HF_HUB_OFFLINE=1 and UV_OFFLINE=1;
  selecting only Alice kept Bob anonymous (cosine ~0.175). SRT export used the
  saved identity snapshot. Those synthetic checks used no private recordings.
- A user-authorized solo meeting clip exposed long-silence contamination in
  enrollment: score 0.392 before filtering, 0.979 afterward with threshold 0.65
  unchanged. MLX and the pinned official ONNX graph agreed on the original
  excerpts (cosine >0.999999), isolating preprocessing rather than weight
  conversion. Same-recording replay is a regression check, not an independent
  speaker-recognition accuracy evaluation. The private audio is not a fixture.

Synthetic artifacts are local scratch data, outside the repo. The global vox
entry is bun-linked to src/main.ts, so local commands use this implementation;
no release or remote deployment is implied.
