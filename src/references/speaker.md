---
type: Guide
title: vox speaker identity
description: Enrollment, matching semantics and storage for vox speaker.* and hear --identify.
---

# Speaker identity (Apple Silicon)

Prerequisites: `uv` and `ffmpeg` on PATH (`brew install uv ffmpeg`). ASR still uses DashScope; voice matching runs locally. The first enrollment or identity request lazily downloads the pinned model (~26 MB) and Python runtime dependencies. Weights live in `~/.vox/models/`, runtime files in `~/.vox/runtime/`, caches in `~/.vox/cache/uv` and `~/.vox/models/huggingface`. No PyTorch needed.

Selectors: `vox @schema '.{speaker,hear}'`.

## Enrolling

Enroll one clean solo recording per person (3–30 seconds; longer files use the first 30 seconds). `start` and `end` select seconds in an existing recording. Enrollment and matching both strip long near-digital silence first, and at least 3 seconds must remain. That is an energy gate, not a speech/noise classifier. Keep enrollment and evaluation recordings separate when checking quality. Re-enroll with `replace: true`.

## Matching

`identify: true` on `hear` turns on ASR speaker separation and matches against all enrolled people; limit candidates with `speakerIds`. MiMo cannot identify speakers (no labels).

The Markdown includes timestamps and names. Anonymous IDs stay in the stored JSON; names and decisions are saved separately as `identified.json`. Uncertain matches keep `Speaker <id>` rather than a forced name. IDs are local to each recording, and several IDs may match one person. `threshold: 0.65` and `margin: 0.1` are starting points, not a calibrated accuracy guarantee.

## Re-identifying

After enrollment or config changes, `speaker.identify` re-matches a stored run with no ASR upload or charge. `export` uses the latest saved identity snapshot, so re-run `speaker.identify` to apply library changes. If identity fails after ASR, the anonymous ASR run remains stored: resume with `session.list` and `speaker.identify`. Nothing outside vox's own run or the explicit `output` is modified.

## Storage

Profiles and settings: `~/.vox/speakers.json` (private permissions). Source enrollment audio is not copied; only the normalized voiceprint is stored. All artifacts use the model version recorded with the profiles; changing embedding models requires re-enrollment.
