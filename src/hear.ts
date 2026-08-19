import { extname, resolve } from "node:path";

import { durationSeconds, isSupportedFormat, supportedFormats } from "./audio.ts";
import { voxError } from "./cli-error.ts";
import { requireApiKey, type AppConfig } from "./config.ts";
import {
  DashScopeClient,
  DIARIZATION_MAX_SECONDS,
  MAX_FILE_BYTES,
  MAX_FILE_SECONDS,
  resolveModel,
} from "./dashscope.ts";
import { tilde } from "./paths.ts";
import {
  digestOf,
  loadRun,
  measure,
  newStore,
  preview,
  runDir,
  saveRun,
  sidOf,
  type Args,
  type RunRecord,
} from "./run-store.ts";
import { contentHash, loadVocabulary, resolveWords, syncVocabulary } from "./vocab.ts";

export const FOLD_TOKENS = 2000;
export const PREVIEW_RUNES = 60;

export type HearInput = {
  file: string;
  model?: string;
  vocab?: string;
  lang?: string[];
  speakers?: boolean;
  refresh?: boolean;
};

export async function hear(app: AppConfig, input: HearInput): Promise<unknown> {
  const apiKey = requireApiKey(app);
  const vendor = input.model ?? "fun";
  const model = resolveModel(vendor);
  if (!model) throw voxError("invalid_usage", `unknown model vendor "${vendor}"`);

  const { data, format, source } = await readAudio(input.file);
  const durationSec = await durationSeconds(source, data);
  checkLimits(data.byteLength, durationSec);
  if (input.speakers && durationSec > DIARIZATION_MAX_SECONDS) {
    console.error(
      `diarization past ${DIARIZATION_MAX_SECONDS / 3600}h may time out rather than degrade`,
    );
  }

  const langs = normalizeLangs(input.lang);
  const args: Args = { model, format, lang: langs.length > 0 ? langs : undefined };
  if (input.speakers) args.speakers = true;

  let vocabularyName = "";
  let loadedVocab: Awaited<ReturnType<typeof loadVocabulary>> | undefined;
  if (input.vocab) {
    loadedVocab = await loadVocabulary(app.dir, input.vocab);
    const resolved = resolveWords(loadedVocab, model);
    for (const warning of resolved.warnings) console.error(warning);
    args.vocab = contentHash(resolved.words);
    vocabularyName = input.vocab;
  }

  const store = newStore(app.dir);
  const digest = digestOf(data, args);
  const sid = sidOf(digest);

  if (!input.refresh) {
    try {
      const rec = await loadRun(store, sid, digest);
      console.error("stored");
      return envelope(rec);
    } catch {
      // miss, digest collision, or corrupt — fall through to recognize
    }
  }
  if (durationSec === 0) {
    console.error("duration unknown; the service will report it");
  }

  const client = new DashScopeClient(apiKey);
  let vocabularyId = "";
  if (loadedVocab && args.vocab) {
    const result = await syncVocabulary(client, app.dir, loadedVocab, model, false);
    for (const warning of result.warnings) console.error(`${loadedVocab.name}: ${warning}`);
    if (result.action !== "reused") console.error(`vocab ${input.vocab} ${result.action}`);
    vocabularyId = result.vocabularyId;
  }

  const t0 = Date.now();
  console.error(`model ${model}${durationSec ? ` ${formatSeconds(durationSec)}` : ""}`);
  let result;
  try {
    result = await client.transcribeFile(
      source.split("/").pop() ?? "audio",
      data,
      {
        model,
        vocabularyId: vocabularyId || undefined,
        languageHints: args.lang,
        diarization: args.speakers,
      },
      taskProgress(),
    );
  } catch (error) {
    throw voxError("api_error", error instanceof Error ? error.message : String(error));
  }
  console.error(`latency ${formatMs(Date.now() - t0)}`);

  const measuredDuration = durationSec || result.duration_sec || 0;
  const rec: RunRecord = {
    digest,
    meta: {
      sid,
      source: tilde(source),
      model,
      vocab: vocabularyName && args.vocab ? `${vocabularyName}@${args.vocab}` : undefined,
      lang: args.lang,
      created: new Date().toISOString(),
      path: tilde(runDir(store, sid)),
      size: measure(result, measuredDuration),
    },
    args,
    result,
  };
  try {
    await saveRun(store, rec, data, format);
  } catch (error) {
    throw voxError(
      "io_error",
      `cannot save run: ${error instanceof Error ? error.message : String(error)}`,
    );
  }
  return envelope(rec);
}

export function envelope(rec: RunRecord): unknown {
  if (rec.meta.size.tokens <= FOLD_TOKENS) {
    return { ...rec.meta, text: rec.result.text };
  }
  return {
    $hints: [
      `Transcript folded at ${FOLD_TOKENS} tokens. Read it with \`vox export --sid ${rec.meta.sid} --format md\`.`,
    ],
    ...rec.meta,
    preview: preview(rec.result.text, PREVIEW_RUNES),
  };
}

async function readAudio(
  file: string,
): Promise<{ data: Uint8Array; format: string; source: string }> {
  const source = resolve(file);
  let data: Uint8Array;
  try {
    data = new Uint8Array(await Bun.file(source).arrayBuffer());
  } catch (error) {
    throw voxError(
      "audio_unsupported",
      `cannot read ${file}: ${error instanceof Error ? error.message : String(error)}`,
    );
  }
  const format = extname(source).slice(1).toLowerCase();
  if (!isSupportedFormat(format)) {
    const label = format ? `.${format}` : `${source.split("/").pop()} (no extension)`;
    throw voxError(
      "audio_unsupported",
      `${label} is not a supported audio format`,
      `supported: ${supportedFormats().join(", ")}`,
    );
  }
  return { data, format, source };
}

export function normalizeLangs(value: string[] | undefined): string[] {
  return (value ?? []).map((item) => item.trim()).filter(Boolean);
}

function checkLimits(bytes: number, durationSec: number): void {
  if (bytes > MAX_FILE_BYTES) {
    throw voxError(
      "audio_too_large",
      `audio is ${(bytes / (1024 * 1024 * 1024)).toFixed(1)}GB, over the ${MAX_FILE_BYTES / (1024 * 1024 * 1024)}GB limit`,
      "re-encode it, or split it",
    );
  }
  if (durationSec > MAX_FILE_SECONDS) {
    throw voxError(
      "audio_too_large",
      `audio is ${formatSeconds(durationSec)}, over the ${MAX_FILE_SECONDS / 3600}h limit`,
      `split the file into parts under ${MAX_FILE_SECONDS / 3600} hours`,
    );
  }
}

function taskProgress(): (status: string, elapsedMs: number) => void {
  let last = "";
  return (status, elapsedMs) => {
    if (status === last) return;
    last = status;
    console.error(`task ${status.toLowerCase()} ${formatMs(elapsedMs)}`);
  };
}

export function formatSeconds(sec: number): string {
  if (sec === 0) return "";
  if (sec >= 3600) return `${Math.floor(sec / 3600)}h${pad2(Math.floor((sec % 3600) / 60))}m`;
  if (sec >= 60) return `${Math.floor(sec / 60)}m${pad2(sec % 60)}s`;
  return `${sec}s`;
}

function formatMs(ms: number): string {
  const sec = Math.round(ms / 1000);
  if (sec < 1) return `${ms}ms`;
  return formatSeconds(sec) || "0s";
}

function pad2(n: number): string {
  return String(n).padStart(2, "0");
}
