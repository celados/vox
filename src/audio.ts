import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { voxError } from "./cli-error.ts";

export const SAMPLE_RATE = 24_000;
export const CHANNEL_COUNT = 1;

const SUPPORTED_FORMATS = new Set([
  "wav",
  "mp3",
  "opus",
  "ogg",
  "m4a",
  "aac",
  "flac",
  "amr",
  "wma",
]);

export function supportedFormats(): string[] {
  return [...SUPPORTED_FORMATS].sort();
}

export function isSupportedFormat(format: string): boolean {
  return SUPPORTED_FORMATS.has(format);
}

export function ceilDiv(a: number, b: number): number {
  if (b === 0) return 0;
  return Math.floor((a + b - 1) / b);
}

/** WAV is parsed inline; everything else needs ffprobe. 0 means unknown. */
export async function durationSeconds(path: string, data: Uint8Array): Promise<number> {
  const wav = wavDuration(data);
  if (wav !== undefined) return wav;
  const probed = await ffprobeDuration(path);
  if (probed !== undefined) return probed;
  return 0;
}

export function wavDuration(data: Uint8Array): number | undefined {
  if (data.length < 44) return undefined;
  const view = new DataView(data.buffer, data.byteOffset, data.byteLength);
  if (ascii(data, 0, 4) !== "RIFF" || ascii(data, 8, 4) !== "WAVE") return undefined;
  let byteRate = 0;
  let pos = 12;
  while (pos + 8 <= data.length) {
    const id = ascii(data, pos, 4);
    const size = view.getUint32(pos + 4, true);
    const body = pos + 8;
    if (id === "fmt " && body + 16 <= data.length) {
      byteRate = view.getUint32(body + 8, true);
    } else if (id === "data" && byteRate > 0) {
      return ceilDiv(size, byteRate);
    }
    pos = body + size;
    if (size % 2 === 1) pos += 1;
  }
  return undefined;
}

async function ffprobeDuration(path: string): Promise<number | undefined> {
  if (!path || path === "mic" || !Bun.which("ffprobe")) return undefined;
  const proc = Bun.spawn(
    [
      "ffprobe",
      "-v",
      "error",
      "-show_entries",
      "format=duration",
      "-of",
      "default=noprint_wrappers=1:nokey=1",
      path,
    ],
    { stdout: "pipe", stderr: "pipe" },
  );
  const [out, code] = await Promise.all([new Response(proc.stdout).text(), proc.exited]);
  if (code !== 0) return undefined;
  const sec = Number.parseFloat(out.trim());
  if (!Number.isFinite(sec) || sec <= 0) return undefined;
  return Math.ceil(sec);
}

export function wrapPcmAsWav(pcm: Uint8Array, sampleRate = SAMPLE_RATE): Uint8Array {
  const dataLen = pcm.byteLength;
  const fileLen = dataLen + 36;
  const byteRate = sampleRate * 2;
  const header = new Uint8Array(44);
  const view = new DataView(header.buffer);
  writeAscii(header, 0, "RIFF");
  view.setUint32(4, fileLen, true);
  writeAscii(header, 8, "WAVE");
  writeAscii(header, 12, "fmt ");
  view.setUint32(16, 16, true);
  view.setUint16(20, 1, true);
  view.setUint16(22, 1, true);
  view.setUint32(24, sampleRate, true);
  view.setUint32(28, byteRate, true);
  view.setUint16(32, 2, true);
  view.setUint16(34, 16, true);
  writeAscii(header, 36, "data");
  view.setUint32(40, dataLen, true);
  const out = new Uint8Array(44 + dataLen);
  out.set(header, 0);
  out.set(pcm, 44);
  return out;
}

export async function writeWav(
  path: string,
  pcm: Uint8Array,
  sampleRate = SAMPLE_RATE,
): Promise<void> {
  await writeFile(path, wrapPcmAsWav(pcm, sampleRate));
}

export async function encodePcmToOpus(pcm: Uint8Array): Promise<Uint8Array> {
  return await ffmpeg(
    [
      "-f",
      "s16le",
      "-ar",
      String(SAMPLE_RATE),
      "-ac",
      String(CHANNEL_COUNT),
      "-i",
      "pipe:0",
      "-c:a",
      "libopus",
      "-b:a",
      "24k",
      "-f",
      "opus",
      "pipe:1",
    ],
    pcm,
  );
}

export async function decodeOpusToPcm(opus: Uint8Array): Promise<Uint8Array> {
  return await ffmpeg(
    [
      "-i",
      "pipe:0",
      "-f",
      "s16le",
      "-ar",
      String(SAMPLE_RATE),
      "-ac",
      String(CHANNEL_COUNT),
      "pipe:1",
    ],
    opus,
  );
}

export async function playPcm(pcm: Uint8Array): Promise<void> {
  const wav = wrapPcmAsWav(pcm);
  const dir = await mkdtemp(join(tmpdir(), "vox-play-"));
  const path = join(dir, "speech.wav");
  await writeFile(path, wav);
  try {
    const player = playbackCommand(path);
    const proc = Bun.spawn(player, { stdout: "ignore", stderr: "pipe" });
    const [stderr, code] = await Promise.all([new Response(proc.stderr).text(), proc.exited]);
    if (code !== 0) {
      throw voxError("io_error", `playback failed: ${stderr.trim() || `exit ${code}`}`);
    }
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
}

export async function recordMicrophone(durationSec: number): Promise<Uint8Array> {
  if (!Bun.which("ffmpeg")) {
    throw voxError(
      "invalid_usage",
      "microphone capture needs ffmpeg, or pass an existing file",
      "vox voice.record --file sample.wav --name myvoice",
    );
  }
  const args =
    process.platform === "darwin"
      ? ["-f", "avfoundation", "-i", ":0"]
      : ["-f", "pulse", "-i", "default"];
  return await ffmpeg([
    "-hide_banner",
    "-loglevel",
    "error",
    ...args,
    "-t",
    String(durationSec),
    "-ar",
    String(SAMPLE_RATE),
    "-ac",
    String(CHANNEL_COUNT),
    "-f",
    "s16le",
    "pipe:1",
  ]);
}

function playbackCommand(path: string): string[] {
  if (process.platform === "darwin" && Bun.which("afplay")) return ["afplay", path];
  if (Bun.which("ffplay")) return ["ffplay", "-nodisp", "-autoexit", "-loglevel", "error", path];
  if (Bun.which("aplay")) return ["aplay", "-q", path];
  throw voxError(
    "io_error",
    "no audio player found (afplay, ffplay, or aplay)",
    "pass --output to write a WAV file instead of playing",
  );
}

async function ffmpeg(args: string[], stdin?: Uint8Array): Promise<Uint8Array> {
  if (!Bun.which("ffmpeg")) throw new Error("ffmpeg is not installed");
  const proc = Bun.spawn(["ffmpeg", "-hide_banner", "-loglevel", "error", ...args], {
    stdin: stdin ?? "ignore",
    stdout: "pipe",
    stderr: "pipe",
  });
  const [out, err, code] = await Promise.all([
    new Response(proc.stdout).bytes(),
    new Response(proc.stderr).text(),
    proc.exited,
  ]);
  if (code !== 0) throw new Error(err.trim() || `ffmpeg exit ${code}`);
  return out;
}

function ascii(data: Uint8Array, start: number, n: number): string {
  return String.fromCharCode(...data.subarray(start, start + n));
}

function writeAscii(target: Uint8Array, offset: number, text: string): void {
  for (let i = 0; i < text.length; i++) {
    target[offset + i] = text.charCodeAt(i);
  }
}
