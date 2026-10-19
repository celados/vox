# SPDX-License-Identifier: Apache-2.0
# Architecture adapted from WeSpeaker (see LICENSE.wespeaker).
# Copyright (c) 2021 Shuai Wang; 2022 Zhengyang Chen; 2023 Bing Han;
# 2024 Zhengyang Chen. MLX implementation and JSON protocol added by vox.
"""Local WeSpeaker embeddings. Protocol: one JSON request and response per process.

Architecture and Kaldi feature settings follow https://github.com/wenet-e2e/wespeaker.
Official ONNX weights are CC-BY-4.0, by the WeSpeaker team:
https://huggingface.co/Wespeaker/wespeaker-voxceleb-resnet34-LM
No ONNX graph or downloaded Python code is executed: only pinned tensor data is read.
"""
import hashlib
import json
import math
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

import mlx.core as mx
import mlx.nn as nn
import numpy as np

MODEL = "wespeaker-resnet34-lm-v1"
REVISION = "f0c48c298fd835726c27956a5d617bad7115627e"
SOURCE_SHA256 = "7bb2f06e9df17cdf1ef14ee8a15ab08ed28e8d0ef5054ee135741560df2ec068"


class VoxError(Exception):
    def __init__(self, code, detail):
        self.code = code
        super().__init__(detail)


def digest(path):
    with open(path, "rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


class Block(nn.Module):
    def __init__(self, incoming, outgoing, stride=1):
        super().__init__()
        self.conv1 = nn.Conv2d(incoming, outgoing, 3, stride=stride, padding=1)
        self.conv2 = nn.Conv2d(outgoing, outgoing, 3, padding=1)
        if stride != 1:
            self.shortcut = nn.Conv2d(incoming, outgoing, 1, stride=stride)

    def __call__(self, x):
        shortcut = self.shortcut(x) if "shortcut" in self else x
        return nn.relu(self.conv2(nn.relu(self.conv1(x))) + shortcut)


class SpeakerModel(nn.Module):
    def __init__(self):
        super().__init__()
        # ONNX export folds batch normalization into each convolution.
        self.conv1 = nn.Conv2d(1, 32, 3, padding=1)
        incoming = 32
        for stage, (channels, count) in enumerate(zip((32, 64, 128, 256), (3, 4, 6, 3)), 1):
            blocks = []
            for index in range(count):
                stride = 2 if stage > 1 and index == 0 else 1
                blocks.append(Block(incoming, channels, stride))
                incoming = channels
            setattr(self, f"layer{stage}", nn.Sequential(*blocks))
        self.fc = nn.Linear(5120, 256)
        self.mean = mx.zeros((256,))

    def __call__(self, features):
        x = features.transpose(0, 2, 1)[..., None]
        x = nn.relu(self.conv1(x))
        for stage in range(1, 5):
            x = getattr(self, f"layer{stage}")(x)
        mean = x.mean(axis=2).transpose(0, 2, 1).reshape(x.shape[0], -1)
        std = mx.sqrt(mx.var(x, axis=2, ddof=1) + 1e-7)
        std = std.transpose(0, 2, 1).reshape(x.shape[0], -1)
        return self.fc(mx.concatenate((mean, std), axis=1)) - self.mean


def convert(source, target):
    import onnx
    from onnx import numpy_helper

    graph = onnx.load(str(source), load_external_data=False).graph
    tensors = {value.name: numpy_helper.to_array(value) for value in graph.initializer}
    paths = ["conv1"]
    for stage, count in enumerate((3, 4, 6, 3), 1):
        for index in range(count):
            prefix = f"layer{stage}.layers.{index}"
            paths.extend((f"{prefix}.conv1", f"{prefix}.conv2"))
            if stage > 1 and index == 0:
                paths.append(f"{prefix}.shortcut")
    nodes = [node for node in graph.node if node.op_type == "Conv"]
    if len(nodes) != len(paths):
        raise VoxError("MODEL_INVALID", "Unexpected official model convolution layout")
    weights = {}
    for name, node in zip(paths, nodes):
        weights[f"{name}.weight"] = mx.array(tensors[node.input[1]].transpose(0, 2, 3, 1).copy())
        weights[f"{name}.bias"] = mx.array(tensors[node.input[2]].copy())
    weights["fc.weight"] = mx.array(tensors["model.seg_1.weight"].copy())
    weights["fc.bias"] = mx.array(tensors["model.seg_1.bias"].copy())
    weights["mean"] = mx.array(tensors["mean_vec"].copy())
    # Strict shape/name validation before persisting a converted model.
    check = SpeakerModel()
    check.load_weights(list(weights.items()), strict=True)
    fd, temporary = tempfile.mkstemp(suffix=".safetensors", dir=target.parent)
    os.close(fd)
    try:
        mx.save_safetensors(temporary, weights)
        os.replace(temporary, target)
    finally:
        Path(temporary).unlink(missing_ok=True)


def load_model(home):
    folder = home / "models" / MODEL
    folder.mkdir(parents=True, exist_ok=True)
    (folder / "NOTICE.txt").write_text(
        "WeSpeaker ResNet34-LM pretrained on VoxCeleb2, by the WeSpeaker team.\n"
        "Source: https://huggingface.co/Wespeaker/wespeaker-voxceleb-resnet34-LM\n"
        "Weights license: CC-BY-4.0 https://creativecommons.org/licenses/by/4.0/\n"
        "Converted from the pinned official ONNX tensor data to MLX safetensors by vox.\n"
    )
    target = folder / "model.safetensors"
    checksum = folder / "model.sha256"
    # An incomplete or damaged conversion is rebuilt from the verified source.
    if not target.is_file() or not checksum.is_file() or digest(target) != checksum.read_text().strip():
        from huggingface_hub import hf_hub_download

        print(f"Downloading/verifying {MODEL}…", file=sys.stderr)
        source = Path(hf_hub_download(
            "Wespeaker/wespeaker-voxceleb-resnet34-LM", "voxceleb_resnet34_LM.onnx",
            revision=REVISION, cache_dir=home / "models" / "huggingface",
        ))
        if digest(source) != SOURCE_SHA256:
            raise VoxError("MODEL_CHECKSUM", "Official model checksum mismatch; remove the corrupted Hugging Face cache and retry")
        convert(source, target)
        fd, temporary = tempfile.mkstemp(dir=folder)
        try:
            with os.fdopen(fd, "w") as stream:
                stream.write(digest(target) + "\n")
            os.replace(temporary, checksum)
        finally:
            Path(temporary).unlink(missing_ok=True)
    model = SpeakerModel()
    model.load_weights(str(target), strict=True)
    model.eval()
    mx.eval(model.parameters())
    return model


def remove_long_silence(wave):
    """Drop >=300ms near-digital silence, preserving 60ms of boundary context.

    Enrollment and sentence excerpts must use the same preprocessing: long
    zero-filled pauses otherwise skew utterance CMN and temporal pooling.
    This conservative energy gate is not a speech/noise classifier.
    """
    if not len(wave):
        return wave
    frame = 320  # 20ms at 16 kHz
    padded = np.pad(wave, (0, (-len(wave)) % frame))
    rms = np.sqrt(np.mean(padded.reshape(-1, frame) ** 2, axis=1))
    active = rms > max(1e-5, float(rms.max()) * 0.001)
    keep = np.ones(len(active), dtype=bool)
    edges = np.flatnonzero(np.diff(np.r_[True, active, True]))
    for start, end in edges.reshape(-1, 2):
        if end - start >= 15:
            keep[start + (3 if start else 0):end - (3 if end < len(active) else 0)] = False
    return wave[np.repeat(keep, frame)[:len(wave)]]


def fbank(wave):
    """16 kHz, 25/10 ms, 80-bin Kaldi hamming fbank, dither=0, CMN.

    First preemphasis sample replicates its predecessor, as Kaldi does.
    Float32 epsilon matches torchaudio's log-energy floor for silent input.
    """
    wave = mx.array(wave) * 32768.0
    count = 1 + (wave.shape[0] - 400) // 160
    frames = mx.as_strided(wave, shape=(count, 400), strides=(160, 1))
    frames = frames - mx.mean(frames, axis=1, keepdims=True)
    frames = mx.concatenate((0.03 * frames[:, :1], frames[:, 1:] - 0.97 * frames[:, :-1]), axis=1)
    window = 0.54 - 0.46 * mx.cos(2 * mx.pi * mx.arange(400) / 399)
    spectrum = mx.abs(mx.fft.rfft(mx.pad(frames * window, ((0, 0), (0, 112))), axis=1)) ** 2
    mel = lambda x: 1127.0 * mx.log(1.0 + x / 700.0)
    low, high = mel(mx.array(20.0)), mel(mx.array(8000.0))
    step = (high - low) / 81
    index = mx.arange(80)[:, None]
    left, center, right = low + index * step, low + (index + 1) * step, low + (index + 2) * step
    frequencies = mel(31.25 * mx.arange(256))[None, :]
    banks = mx.maximum(0, mx.minimum((frequencies - left) / (center - left), (right - frequencies) / (right - center)))
    banks = mx.pad(banks, ((0, 0), (0, 1)))
    result = mx.log(mx.maximum(spectrum @ banks.T, np.finfo(np.float32).eps))
    return result - result.mean(axis=0, keepdims=True)


def decode(audio, start, duration):
    executable = shutil.which("ffmpeg")
    if not executable:
        raise VoxError("FFMPEG_MISSING", "ffmpeg is required for speaker embeddings; install it with brew install ffmpeg")
    command = [executable, "-v", "error", "-nostdin", "-ss", str(start), "-i", str(audio), "-t", str(duration), "-vn", "-ar", "16000", "-ac", "1", "-f", "f32le", "pipe:1"]
    result = subprocess.run(command, capture_output=True, timeout=120)
    if result.returncode:
        raise VoxError("AUDIO_DECODE", result.stderr.decode(errors="replace").strip()[:1000])
    return np.frombuffer(result.stdout, dtype="<f4").copy()


def run(request):
    audio = Path(request["audio"]).expanduser().resolve()
    if not audio.is_file():
        raise VoxError("AUDIO_NOT_FOUND", f"Audio file does not exist: {audio}")
    home = Path(request.get("home") or os.environ.get("VOX_HOME") or "~/.vox").expanduser().resolve()
    segments = request.get("segments") or [{"id": "audio", "start": 0, "end": 30}]
    if not isinstance(segments, list) or len(segments) > 1000:
        raise VoxError("INVALID_REQUEST", "segments must contain at most 1000 entries")
    validated = []
    for segment in segments:
        start, end = float(segment["start"]), float(segment["end"])
        if not math.isfinite(start) or not math.isfinite(end) or start < 0 or not 3 <= end - start <= 30:
            raise VoxError("INVALID_SEGMENT", "Each segment must have finite, nonnegative times and a duration of 3–30 seconds")
        validated.append((str(segment["id"]), start, end))
    model = None
    embeddings = []
    for identifier, start, end in validated:
        wave = decode(audio, start, end - start)
        if not np.isfinite(wave).all() or not len(wave) or float(np.sqrt(np.mean(wave ** 2))) < 1e-5:
            raise VoxError("AUDIO_SILENT", f"Segment {identifier} is silent or invalid")
        wave = remove_long_silence(wave)
        duration = len(wave) / 16000
        if duration < 3:
            raise VoxError("AUDIO_TOO_SHORT", f"Segment {identifier} contains {duration:.3f}s after removing long silence; at least 3s is required")
        if model is None:
            model = load_model(home)
        vector = model(fbank(wave)[None, ...])[0]
        vector = vector / mx.maximum(mx.linalg.norm(vector), 1e-12)
        mx.eval(vector)
        values = np.array(vector)
        if not np.isfinite(values).all():
            raise VoxError("EMBEDDING_INVALID", "Model produced nonfinite values")
        embeddings.append({"id": identifier, "vector": values.tolist(), "duration": duration})
    return {"model": MODEL, "embeddings": embeddings}


if __name__ == "__main__":
    try:
        print(json.dumps(run(json.load(sys.stdin)), allow_nan=False))
    except Exception as error:
        print(json.dumps({"error": {"code": getattr(error, "code", "RUNTIME_ERROR"), "detail": str(error)}}))
        sys.exit(1)
