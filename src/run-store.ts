import { createHash } from "node:crypto";
import { mkdir, mkdtemp, readdir, readFile, rename, rm, stat, writeFile } from "node:fs/promises";
import { join, resolve } from "node:path";

import { parse, stringify } from "yaml";

import { asrWords, type AsrResult } from "./dashscope.ts";
import { voxError } from "./cli-error.ts";
import { goJSON } from "./go-json.ts";
import { expandHome, tilde } from "./paths.ts";

export const SID_LENGTH = 12;
const SID_PATTERN = new RegExp(`^[0-9a-f]{1,${SID_LENGTH}}$`);

export type Args = {
  model: string;
  format: string;
  speakers?: boolean;
  vocab?: string;
  lang?: string[];
};

export type Size = {
  tokens: number;
  words: number;
  chars: number;
  duration: number;
};

export type Meta = {
  sid: string;
  source: string;
  model: string;
  vocab?: string;
  lang?: string[];
  created: string;
  path: string;
  size: Size;
};

export type RunRecord = {
  meta: Meta;
  args: Args;
  digest: string;
  result: AsrResult;
};

export type Store = {
  dir: string;
};

export function newStore(voxDir: string): Store {
  return { dir: join(voxDir, "runs") };
}

export function runDir(store: Store, sid: string): string {
  return join(store.dir, sid);
}

/** Field order and omitempty must match Go encoding/json of run.Args. */
export function canonicalArgsJson(args: Args): string {
  const obj: Record<string, unknown> = {
    model: args.model,
    format: args.format,
  };
  if (args.speakers) obj.speakers = true;
  if (args.vocab) obj.vocab = args.vocab;
  if (args.lang && args.lang.length > 0) obj.lang = args.lang;
  return goJSON(obj);
}

export function digestOf(audio: Uint8Array, args: Args): string {
  const hash = createHash("sha256");
  hash.update(audio);
  hash.update(canonicalArgsJson(args));
  return hash.digest("hex");
}

export function sidOf(digest: string): string {
  return digest.slice(0, SID_LENGTH);
}

export async function loadRun(store: Store, sid: string, digest = ""): Promise<RunRecord> {
  const data = await readFile(join(runDir(store, sid), "run.json"), "utf8");
  const rec = JSON.parse(data) as RunRecord;
  if (digest && rec.digest !== digest) {
    throw new Error(`run ${sid} holds a different input`);
  }
  return rec;
}

export async function saveRun(
  store: Store,
  rec: RunRecord,
  audio: Uint8Array,
  format: string,
): Promise<void> {
  await mkdir(store.dir, { recursive: true });
  const staging = await mkdtemp(join(store.dir, ".staging-"));
  try {
    await writeFile(join(staging, `audio.${format}`), audio);
    await writeFile(join(staging, "meta.yaml"), stringify(metaForYaml(rec.meta)));
    await writeFile(join(staging, "run.json"), JSON.stringify(rec, null, 2) + "\n");
    const final = runDir(store, rec.meta.sid);
    await rm(final, { recursive: true, force: true });
    await rename(staging, final);
  } catch (error) {
    await rm(staging, { recursive: true, force: true });
    throw error;
  }
}

export async function resolveSid(store: Store, prefix: string): Promise<string> {
  if (!SID_PATTERN.test(prefix)) {
    throw voxError(
      "session_not_found",
      `"${prefix}" is not a run id: expected up to ${SID_LENGTH} hex characters`,
    );
  }
  try {
    await stat(runDir(store, prefix));
    return prefix;
  } catch {
    // prefix is shorter than a full sid; scan for a unique match
  }
  let entries: string[];
  try {
    entries = await readdir(store.dir);
  } catch (error) {
    if (isNotFound(error)) {
      throw voxError("session_not_found", `no run matching "${prefix}"`, "vox session.list");
    }
    throw error;
  }
  const matches = entries.filter((name) => SID_PATTERN.test(name) && name.startsWith(prefix));
  if (matches.length === 0) {
    throw voxError("session_not_found", `no run matching "${prefix}"`, "vox session.list");
  }
  if (matches.length === 1) return matches[0]!;
  throw voxError(
    "session_ambiguous",
    `"${prefix}" matches ${matches.length} runs: ${matches.join(", ")}`,
  );
}

export async function listRuns(store: Store, sourceFilter = "", limit = 0): Promise<Meta[]> {
  let names: string[];
  try {
    names = await readdir(store.dir);
  } catch (error) {
    if (isNotFound(error)) return [];
    throw error;
  }

  const metas: Meta[] = [];
  for (const name of names) {
    if (!SID_PATTERN.test(name)) continue;
    try {
      const data = await readFile(join(store.dir, name, "meta.yaml"), "utf8");
      const meta = normalizeMeta(parse(data));
      if (sourceFilter && !sameSource(meta.source, sourceFilter)) continue;
      metas.push(meta);
    } catch {
      continue;
    }
  }
  metas.sort((left, right) =>
    left.created < right.created ? 1 : left.created > right.created ? -1 : 0,
  );
  if (limit > 0 && metas.length > limit) return metas.slice(0, limit);
  return metas;
}

export async function removeRun(store: Store, sid: string): Promise<void> {
  await rm(runDir(store, sid), { recursive: true, force: true });
}

export async function removeAllRuns(store: Store): Promise<void> {
  await rm(store.dir, { recursive: true, force: true });
}

export function sameSource(recorded: string, filter: string): boolean {
  if (recorded === filter) return true;
  try {
    return resolve(expandHome(recorded)) === resolve(expandHome(filter));
  } catch {
    return false;
  }
}

export { tilde };

export function measure(result: AsrResult, durationSec: number): Size {
  let cjk = 0;
  let other = 0;
  for (const ch of result.text) {
    if (isCjk(ch)) cjk += 1;
    else other += 1;
  }
  return {
    tokens: cjk + Math.floor((other + 3) / 4),
    words: asrWords(result).length,
    chars: [...result.text].length,
    duration: durationSec,
  };
}

export function preview(text: string, runes: number): string {
  const chars = [...text];
  if (chars.length <= runes) return text;
  return chars.slice(0, runes).join("") + "…";
}

function isCjk(ch: string): boolean {
  return /\p{Script=Han}|\p{Script=Hiragana}|\p{Script=Katakana}|\p{Script=Hangul}/u.test(ch);
}

function metaForYaml(meta: Meta): Record<string, unknown> {
  const out: Record<string, unknown> = {
    sid: meta.sid,
    source: meta.source,
    model: meta.model,
  };
  if (meta.vocab) out.vocab = meta.vocab;
  if (meta.lang && meta.lang.length > 0) out.lang = meta.lang;
  out.created = meta.created;
  out.path = meta.path;
  out.size = meta.size;
  return out;
}

function normalizeMeta(raw: unknown): Meta {
  const obj = raw as Record<string, unknown>;
  const size = (obj.size ?? {}) as Record<string, unknown>;
  const created = obj.created;
  return {
    sid: String(obj.sid ?? ""),
    source: String(obj.source ?? ""),
    model: String(obj.model ?? ""),
    vocab: obj.vocab ? String(obj.vocab) : undefined,
    lang: Array.isArray(obj.lang) ? obj.lang.map(String) : undefined,
    created: created instanceof Date ? created.toISOString() : String(created ?? ""),
    path: String(obj.path ?? ""),
    size: {
      tokens: Number(size.tokens ?? 0),
      words: Number(size.words ?? 0),
      chars: Number(size.chars ?? 0),
      duration: Number(size.duration ?? 0),
    },
  };
}

function isNotFound(error: unknown): boolean {
  return typeof error === "object" && error !== null && "code" in error && error.code === "ENOENT";
}
