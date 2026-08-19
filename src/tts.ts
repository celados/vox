import { createHash } from "node:crypto";
import { mkdir, readdir, readFile, rm, stat, writeFile } from "node:fs/promises";
import { join } from "node:path";

import {
  decodeOpusToPcm,
  encodePcmToOpus,
  playPcm,
  recordMicrophone,
  wrapPcmAsWav,
  writeWav,
} from "./audio.ts";
import { voxError } from "./cli-error.ts";
import { requireApiKey, saveState, type AppConfig } from "./config.ts";
import {
  DashScopeClient,
  isSystemVoice,
  MODEL_INSTRUCT_REALTIME,
  modelForVoice,
  SYSTEM_VOICES,
} from "./dashscope.ts";
import { streamTts } from "./realtime.ts";
import { resolveTextField } from "./text-body.ts";

export type SayInput = {
  text: string;
  voice?: string;
  lang?: string;
  instruct?: string;
  speed?: number;
  output?: string;
  noCache?: boolean;
};

export async function say(app: AppConfig, input: SayInput): Promise<unknown> {
  const apiKey = requireApiKey(app);
  const text = await resolveTextField(input.text);
  if (!text?.trim()) throw voxError("invalid_usage", "say requires text");

  let voice = input.voice || app.state.last_voice || "Cherry";
  let model = modelForVoice(voice);
  if (input.instruct && isSystemVoice(voice)) model = MODEL_INSTRUCT_REALTIME;

  const lang = input.lang ?? "auto";
  const speed = input.speed ?? 1.0;
  const instruct = input.instruct ?? "";
  const cacheKey = `${model}:${voice}:${lang}:${instruct}:${text}:${speed.toFixed(1)}`;
  const hash = createHash("sha256").update(cacheKey).digest("hex");
  const cacheDir = join(app.dir, "cache");
  await mkdir(cacheDir, { recursive: true });
  const opusPath = join(cacheDir, `${hash}.opus`);
  const pcmPath = join(cacheDir, `${hash}.pcm`);

  let pcm: Uint8Array | undefined;
  let cached = false;
  if (!input.noCache) {
    pcm = await readCachedPcm(opusPath, pcmPath);
    cached = pcm !== undefined;
    if (cached) console.error(`cached ${voice}`);
  }

  if (!pcm) {
    console.error(`voice ${voice} (${model})`);
    const chunks: Uint8Array[] = [];
    const t0 = Date.now();
    let first = false;
    try {
      await streamTts(
        apiKey,
        {
          model,
          voice,
          text,
          lang,
          instruct: instruct || undefined,
          speechRate: speed,
        },
        (chunk) => {
          if (!first) {
            first = true;
            console.error(`first audio ${Date.now() - t0}ms`);
          }
          chunks.push(chunk);
        },
      );
    } catch (error) {
      throw voxError("api_error", error instanceof Error ? error.message : String(error));
    }
    pcm = concat(chunks);
    if (!input.noCache && pcm.byteLength > 0) {
      try {
        await writeFile(opusPath, await encodePcmToOpus(pcm));
      } catch {
        await writeFile(pcmPath, pcm);
      }
    }
  }

  if (!input.output) await playPcm(pcm);
  if (input.output) await writeWav(input.output, pcm);

  app.state.last_voice = voice;
  if (lang !== "auto") app.state.last_lang = lang;
  await saveState(app);

  const out: Record<string, unknown> = { voice, model, cached, bytes: pcm.byteLength };
  if (input.output) out.output = input.output;
  return out;
}

export async function listVoices(app: AppConfig): Promise<unknown> {
  const system = SYSTEM_VOICES.map((voice) => ({
    id: voice.id,
    gender: voice.gender,
    language: voice.language,
  }));
  let cloned: Array<{ name: string; id: string; language?: string; model?: string }> = [];
  try {
    const apiKey = requireApiKey(app);
    const voices = await new DashScopeClient(apiKey).listVoices(0, 50);
    cloned = voices.map((voice) => ({
      name: extractNameFromVoiceId(voice.voice),
      id: voice.voice,
      language: voice.language,
      model: voice.target_model,
    }));
  } catch (error) {
    if (error instanceof Error && error.name === "not_authenticated") {
      return { system, cloned: [], $hints: ["vox auth.login to see cloned voices"] };
    }
    console.error(
      `failed to fetch cloned voices: ${error instanceof Error ? error.message : String(error)}`,
    );
  }
  return { system, cloned };
}

const SAMPLE_TEXTS: Record<string, string> = {
  zh: "今天天气真不错，适合出去走走。技术正在以前所未有的速度发展，改变着我们的生活方式。",
  en: "The quick brown fox jumps over the lazy dog. Technology is evolving faster than ever before, reshaping how we live and work.",
  ja: "今日はとても良い天気ですね。テクノロジーはかつてないスピードで進化しています。私たちの生活を大きく変えています。",
};

export type VoiceRecordInput = {
  name?: string;
  file?: string;
  lang?: string;
  duration?: number;
};

export async function recordVoice(app: AppConfig, input: VoiceRecordInput): Promise<unknown> {
  const apiKey = requireApiKey(app);
  const lang = input.lang || (await detectSystemLang());
  const duration = input.duration ?? 15;

  let wavData: Uint8Array;
  if (input.file) {
    wavData = new Uint8Array(await Bun.file(input.file).arrayBuffer());
    console.error(`using ${input.file}`);
  } else {
    if (!process.stdin.isTTY) {
      throw voxError(
        "invalid_usage",
        "microphone capture needs a TTY, or pass --file",
        "vox voice.record --file sample.wav --name myvoice",
      );
    }
    const sample = SAMPLE_TEXTS[lang] ?? SAMPLE_TEXTS.en!;
    console.error(`Read this aloud:\n  ${sample}\nRecording for ${duration}s...`);
    const pcm = await recordMicrophone(duration);
    console.error(`recorded ${pcm.byteLength} bytes`);
    wavData = wrapPcmAsWav(pcm);
    const local = join(app.dir, "voices", `recording-${Math.floor(Date.now() / 1000)}.wav`);
    await mkdir(join(app.dir, "voices"), { recursive: true });
    await writeFile(local, wavData);
  }

  const name = input.name || `vox${Date.now() % 10_000_000_000}`;
  console.error(`enrolling ${name}...`);
  let voiceId: string;
  try {
    voiceId = await new DashScopeClient(apiKey).enrollVoice(
      name,
      Buffer.from(wavData).toString("base64"),
    );
  } catch (error) {
    throw voxError("api_error", error instanceof Error ? error.message : String(error));
  }

  app.state.last_voice = voiceId;
  await saveState(app);
  return {
    voiceId,
    name,
    $hints: [`vox say --voice ${voiceId} --text 'Hello!'`],
  };
}

export async function removeVoice(app: AppConfig, voiceId: string): Promise<unknown> {
  const apiKey = requireApiKey(app);
  if (isSystemVoice(voiceId)) {
    throw voxError("invalid_usage", `cannot delete system voice: ${voiceId}`);
  }
  try {
    await new DashScopeClient(apiKey).deleteVoice(voiceId);
  } catch (error) {
    throw voxError("api_error", error instanceof Error ? error.message : String(error));
  }
  if (app.state.last_voice === voiceId) {
    app.state.last_voice = undefined;
    await saveState(app);
  }
  return { removed: voiceId };
}

export async function cacheStatus(app: AppConfig): Promise<unknown> {
  const dir = join(app.dir, "cache");
  let entries: string[];
  try {
    entries = await readdir(dir);
  } catch {
    return { path: dir, files: 0, bytes: 0 };
  }
  let bytes = 0;
  for (const name of entries) {
    try {
      bytes += (await stat(join(dir, name))).size;
    } catch {
      continue;
    }
  }
  return { path: dir, files: entries.length, bytes };
}

export async function cacheClear(app: AppConfig): Promise<unknown> {
  const dir = join(app.dir, "cache");
  let entries: string[];
  try {
    entries = await readdir(dir);
  } catch {
    return { cleared: 0 };
  }
  let count = 0;
  for (const name of entries) {
    try {
      await rm(join(dir, name));
      count += 1;
    } catch {
      continue;
    }
  }
  return { cleared: count };
}

export function extractNameFromVoiceId(id: string): string {
  const prefix = "qwen-tts-vc-";
  const marker = "-voice-";
  if (!id.startsWith(prefix)) return id;
  const rest = id.slice(prefix.length);
  const idx = rest.indexOf(marker);
  if (idx < 0) return id;
  return rest.slice(0, idx);
}

async function readCachedPcm(opusPath: string, pcmPath: string): Promise<Uint8Array | undefined> {
  try {
    const opus = await readFile(opusPath);
    return await decodeOpusToPcm(opus);
  } catch {
    try {
      return await readFile(pcmPath);
    } catch {
      return undefined;
    }
  }
}

async function detectSystemLang(): Promise<string> {
  const localeToLang: Record<string, string> = {
    zh: "zh",
    en: "en",
    ja: "ja",
    ko: "ko",
    de: "de",
    fr: "fr",
    es: "es",
    pt: "pt",
    it: "it",
    ru: "ru",
    pl: "pl",
    sv: "sv",
    da: "da",
    fi: "fi",
    nb: "no",
    nn: "no",
    cs: "cs",
    is: "is",
  };
  if (process.platform !== "darwin") return "en";
  const proc = Bun.spawn(["defaults", "read", "-g", "AppleLocale"], {
    stdout: "pipe",
    stderr: "ignore",
  });
  const [out, code] = await Promise.all([new Response(proc.stdout).text(), proc.exited]);
  if (code !== 0) return "en";
  const prefix = out.trim().split("_")[0] ?? "en";
  return localeToLang[prefix] ?? "en";
}

function concat(chunks: Uint8Array[]): Uint8Array {
  const total = chunks.reduce((sum, chunk) => sum + chunk.byteLength, 0);
  const out = new Uint8Array(total);
  let offset = 0;
  for (const chunk of chunks) {
    out.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return out;
}
