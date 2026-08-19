import { createHash } from "node:crypto";
import { mkdir, readdir, readFile, rename, writeFile } from "node:fs/promises";
import { join } from "node:path";

import { parse } from "yaml";

import { VoxError, voxError } from "./cli-error.ts";
import {
  DashScopeClient,
  MODEL_FUN_ASR,
  superHotwordsSupported,
  VOCABULARY_QUOTA,
  type Hotword,
} from "./dashscope.ts";
import { goJSON, sortUtf8 } from "./go-json.ts";
import { tilde } from "./paths.ts";

export const DEFAULT_WEIGHT = 4;
export const SUPER_WEIGHT = 50;
export const MAX_WORDS = 2000;
export const MAX_SUPER_WORDS = 50;
const DEPLOY_TIMEOUT_MS = 30_000;

const ROOT_KEYS = new Set(["lang", "default_weight", "words", "models"]);
const MODEL_KEYS = new Set(["words"]);
const FUN_ASR_LANGS = new Set(["zh", "en", "ja"]);

export type VocabFile = {
  lang?: string;
  default_weight?: number;
  words: Record<string, number | null>;
  models?: Record<string, { words: Record<string, number | null> }>;
};

export type Vocabulary = {
  name: string;
  path: string;
  file: VocabFile;
};

export type IndexEntry = {
  vocabulary_id: string;
  content_hash: string;
  synced_at: string;
};

export type Index = Record<string, Record<string, IndexEntry>>;

export type SyncResult = {
  vocabularyId: string;
  contentHash: string;
  action: "reused" | "created" | "updated";
  warnings: string[];
};

export function vocabDir(voxDir: string): string {
  return join(voxDir, "vocabulary");
}

export function vocabPath(voxDir: string, name: string): string {
  return join(vocabDir(voxDir), `${name}.yaml`);
}

export async function loadVocabulary(voxDir: string, name: string): Promise<Vocabulary> {
  const path = vocabPath(voxDir, name);
  let text: string;
  try {
    text = await readFile(path, "utf8");
  } catch (error) {
    if (isNotFound(error)) {
      throw voxError(
        "vocab_not_found",
        `no vocabulary "${name}"`,
        `write ${tilde(path)}, then rerun`,
      );
    }
    throw error;
  }
  try {
    const file = parseVocabYaml(text, name);
    return { name, path, file };
  } catch (error) {
    if (error instanceof Error && error.name === "vocab_not_found") throw error;
    throw voxError(
      "vocab_not_found",
      `vocabulary "${name}" is not valid: ${error instanceof Error ? error.message : String(error)}`,
      `check ${tilde(path)}`,
    );
  }
}

export async function vocabNames(voxDir: string): Promise<string[]> {
  let entries: string[];
  try {
    entries = await readdir(vocabDir(voxDir));
  } catch (error) {
    if (isNotFound(error)) return [];
    throw error;
  }
  const names: string[] = [];
  for (const entry of entries) {
    if (entry.startsWith(".") || !entry.endsWith(".yaml")) continue;
    names.push(entry.slice(0, -5));
  }
  names.sort();
  return names;
}

export async function listVocabularies(voxDir: string): Promise<Vocabulary[]> {
  const names = await vocabNames(voxDir);
  const out: Vocabulary[] = [];
  for (const name of names) out.push(await loadVocabulary(voxDir, name));
  return out;
}

export function resolveWords(
  vocab: Vocabulary,
  model: string,
): { words: Hotword[]; warnings: string[] } {
  const merged: Record<string, number | null> = { ...vocab.file.words };
  const block = vocab.file.models?.[model];
  if (block) Object.assign(merged, block.words);

  const defaultWeight = vocab.file.default_weight || DEFAULT_WEIGHT;
  const warnings: string[] = [];
  const lang = langFor(model, vocab.file.lang ?? "");
  if (vocab.file.lang && !lang) {
    warnings.push(
      `lang "${vocab.file.lang}" is not supported by ${model}; letting the model auto-detect`,
    );
  }

  const names = sortUtf8(Object.keys(merged));
  const words: Hotword[] = [];
  let superCount = 0;
  let clamped = 0;
  let oversized = 0;

  for (const name of names) {
    const word = name.trim();
    if (!word) {
      warnings.push("dropped a blank word entry");
      continue;
    }
    let weight = merged[name] ?? defaultWeight;
    if (weight === SUPER_WEIGHT) {
      if (!superHotwordsSupported(model) || superCount >= MAX_SUPER_WORDS) {
        weight = 5;
        clamped += 1;
      } else {
        superCount += 1;
      }
    }
    if (!validWeight(weight)) {
      warnings.push(`"${word}": weight ${weight} out of range, using ${DEFAULT_WEIGHT}`);
      weight = DEFAULT_WEIGHT;
    }
    if (!validLength(word)) {
      oversized += 1;
      continue;
    }
    const hotword: Hotword = { text: word, weight };
    if (lang) hotword.lang = lang;
    words.push(hotword);
  }

  if (clamped > 0) {
    warnings.push(`${clamped} super hotword(s) clamped to weight 5 for ${model}`);
  }
  if (oversized > 0) {
    warnings.push(`${oversized} word(s) dropped: over the length limit`);
  }
  if (words.length > MAX_WORDS) {
    warnings.push(
      `${words.length} words exceed the ${MAX_WORDS} cap; keeping the first ${MAX_WORDS}`,
    );
    return { words: words.slice(0, MAX_WORDS), warnings };
  }
  return { words, warnings };
}

export function contentHash(words: Hotword[]): string {
  const canonical = words.map((word) => {
    const obj: Record<string, unknown> = { text: word.text, weight: word.weight };
    if (word.lang) obj.lang = word.lang;
    return obj;
  });
  const sum = createHash("sha256").update(goJSON(canonical)).digest("hex");
  return sum.slice(0, 8);
}

export function prefixFor(name: string): string {
  let prefix = name.toLowerCase().replace(/[^a-z0-9]/g, "");
  if (!prefix) prefix = "vox";
  if (prefix.length > 10) prefix = prefix.slice(0, 10);
  return prefix;
}

export function validWeight(weight: number): boolean {
  return (weight >= 1 && weight <= 5) || weight === SUPER_WEIGHT;
}

export function validLength(word: string): boolean {
  const runes = [...word];
  if (runes.length === 0 || word.trim().split(/\s+/).filter(Boolean).length === 0) return false;
  for (const ch of runes) {
    if ((ch.codePointAt(0) ?? 0) > 127) return runes.length <= 15;
  }
  return word.trim().split(/\s+/).length <= 7;
}

function langFor(model: string, lang: string): string {
  if (!lang) return "";
  if (model === MODEL_FUN_ASR && !FUN_ASR_LANGS.has(lang)) return "";
  return lang;
}

export function parseVocabYaml(text: string, name: string): VocabFile {
  const raw = parse(text) as unknown;
  if (raw === null || raw === undefined) return { words: {} };
  if (typeof raw !== "object" || Array.isArray(raw)) {
    throw new Error("expected a mapping");
  }
  const obj = raw as Record<string, unknown>;
  assertKeys(obj, ROOT_KEYS, name);
  const defaultWeight = obj.default_weight;
  if (typeof defaultWeight === "number" && defaultWeight !== 0 && !validWeight(defaultWeight)) {
    throw voxError(
      "vocab_not_found",
      `vocabulary "${name}" has default_weight ${defaultWeight}; expected 1-5`,
      `check ${name}.yaml`,
    );
  }
  return {
    lang: typeof obj.lang === "string" ? obj.lang : undefined,
    default_weight: typeof defaultWeight === "number" ? defaultWeight : undefined,
    words: parseWordMap(obj.words),
    models: parseModels(obj.models, name),
  };
}

function parseWordMap(raw: unknown): Record<string, number | null> {
  if (raw === undefined || raw === null) return {};
  if (typeof raw !== "object" || Array.isArray(raw)) throw new Error("words must be a mapping");
  const out: Record<string, number | null> = {};
  for (const [word, weight] of Object.entries(raw as Record<string, unknown>)) {
    if (weight === null || weight === undefined) {
      out[word] = null;
    } else if (typeof weight === "number") {
      out[word] = weight;
    } else {
      throw new Error(`word "${word}" has a non-numeric weight`);
    }
  }
  return out;
}

function parseModels(
  raw: unknown,
  name: string,
): Record<string, { words: Record<string, number | null> }> | undefined {
  if (raw === undefined || raw === null) return undefined;
  if (typeof raw !== "object" || Array.isArray(raw)) throw new Error("models must be a mapping");
  const out: Record<string, { words: Record<string, number | null> }> = {};
  for (const [model, block] of Object.entries(raw as Record<string, unknown>)) {
    if (typeof block !== "object" || block === null || Array.isArray(block)) {
      throw new Error(`models.${model} must be a mapping`);
    }
    const row = block as Record<string, unknown>;
    assertKeys(row, MODEL_KEYS, name);
    out[model] = { words: parseWordMap(row.words) };
  }
  return out;
}

function assertKeys(obj: Record<string, unknown>, allowed: Set<string>, name: string): void {
  for (const key of Object.keys(obj)) {
    if (!allowed.has(key)) {
      throw voxError(
        "vocab_not_found",
        `vocabulary "${name}" is not valid: unknown field ${key}`,
        `check ${name}.yaml`,
      );
    }
  }
}

function indexPath(voxDir: string): string {
  return join(vocabDir(voxDir), ".index.json");
}

export async function loadIndex(voxDir: string): Promise<Index> {
  const path = indexPath(voxDir);
  let data: string;
  try {
    data = await readFile(path, "utf8");
  } catch (error) {
    if (isNotFound(error)) return {};
    throw error;
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(data);
  } catch (error) {
    throw indexCorrupt(path, error);
  }
  const idx = asIndex(parsed);
  if (!idx) {
    throw voxError(
      "vocab_index_corrupt",
      `${tilde(path)} is not a vocabulary index`,
      "inspect it, or delete it and re-run `vox vocab.sync --all`",
    );
  }
  return idx;
}

function indexCorrupt(path: string, error: unknown): never {
  throw voxError(
    "vocab_index_corrupt",
    `${tilde(path)} is not readable: ${error instanceof Error ? error.message : String(error)}`,
    "inspect it, or delete it and re-run `vox vocab.sync --all`",
  );
}

/** Fail closed: a JSON array/string/number must not look like an empty index. */
export function asIndex(value: unknown): Index | undefined {
  if (!isPlainObject(value)) return undefined;
  const out: Index = {};
  for (const [name, models] of Object.entries(value)) {
    if (!isPlainObject(models)) return undefined;
    const row: Record<string, IndexEntry> = {};
    for (const [model, entry] of Object.entries(models)) {
      const parsed = asIndexEntry(entry);
      if (!parsed) return undefined;
      row[model] = parsed;
    }
    out[name] = row;
  }
  return out;
}

function asIndexEntry(value: unknown): IndexEntry | undefined {
  if (!isPlainObject(value)) return undefined;
  if (typeof value.vocabulary_id !== "string" || typeof value.content_hash !== "string") {
    return undefined;
  }
  return {
    vocabulary_id: value.vocabulary_id,
    content_hash: value.content_hash,
    synced_at: typeof value.synced_at === "string" ? value.synced_at : "",
  };
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

async function writeIndex(voxDir: string, idx: Index): Promise<void> {
  try {
    await saveIndex(voxDir, idx);
  } catch (error) {
    throw voxError(
      "io_error",
      `cannot write vocabulary index: ${error instanceof Error ? error.message : String(error)}`,
    );
  }
}

export async function saveIndex(voxDir: string, idx: Index): Promise<void> {
  const dir = vocabDir(voxDir);
  await mkdir(dir, { recursive: true });
  const tmp = join(dir, `.index-${process.pid}-${Date.now()}.json`);
  await writeFile(tmp, JSON.stringify(idx, null, 2) + "\n");
  await rename(tmp, indexPath(voxDir));
}

export function indexGet(idx: Index, name: string, model: string): IndexEntry | undefined {
  return idx[name]?.[model];
}

export function indexSet(idx: Index, name: string, model: string, entry: IndexEntry): void {
  idx[name] ??= {};
  idx[name][model] = entry;
}

export function claimedIds(idx: Index, existing: Set<string>): Record<string, true> {
  const ids: Record<string, true> = {};
  for (const [name, models] of Object.entries(idx)) {
    if (!existing.has(name)) continue;
    for (const entry of Object.values(models)) ids[entry.vocabulary_id] = true;
  }
  return ids;
}

export function forgetUnbacked(idx: Index, existing: Set<string>): boolean {
  let changed = false;
  for (const name of Object.keys(idx)) {
    if (!existing.has(name)) {
      delete idx[name];
      changed = true;
    }
  }
  return changed;
}

export function describeIndex(idx: Index, name: string): Record<string, string> {
  const state: Record<string, string> = {};
  for (const [model, entry] of Object.entries(idx[name] ?? {})) {
    state[model] = `${entry.vocabulary_id}@${entry.content_hash}`;
  }
  return state;
}

export async function syncVocabulary(
  client: DashScopeClient,
  voxDir: string,
  vocab: Vocabulary,
  model: string,
  force: boolean,
): Promise<SyncResult> {
  const { words, warnings } = resolveWords(vocab, model);
  if (words.length === 0) {
    throw voxError(
      "vocab_not_found",
      `vocabulary "${vocab.name}" resolves to no usable words for ${model}`,
      `check ${tilde(vocab.path)}`,
    );
  }
  const hash = contentHash(words);
  const idx = await loadIndex(voxDir);
  const result: SyncResult = { vocabularyId: "", contentHash: hash, action: "created", warnings };

  const entry = indexGet(idx, vocab.name, model);
  if (entry) {
    let bound = false;
    try {
      await verifyBinding(client, entry.vocabulary_id, model);
      bound = true;
    } catch (error) {
      if (error instanceof VoxError && error.code === "vocab_model_mismatch") throw error;
      result.warnings.push(`stored list ${entry.vocabulary_id} is gone; recreating`);
      const models = idx[vocab.name];
      if (models) delete models[model];
    }
    if (bound) {
      if (entry.content_hash === hash && !force) {
        result.vocabularyId = entry.vocabulary_id;
        result.action = "reused";
        return result;
      }
      await wrapApi(() => client.updateVocabulary(entry.vocabulary_id, words));
      await wrapApi(() => client.awaitVocabulary(entry.vocabulary_id, DEPLOY_TIMEOUT_MS));
      indexSet(idx, vocab.name, model, {
        vocabulary_id: entry.vocabulary_id,
        content_hash: hash,
        synced_at: new Date().toISOString(),
      });
      await writeIndex(voxDir, idx);
      result.vocabularyId = entry.vocabulary_id;
      result.action = "updated";
      return result;
    }
  }

  const remote = await wrapApi(() => client.listVocabularies());
  if (remote.length >= VOCABULARY_QUOTA) {
    throw voxError(
      "vocab_quota_exceeded",
      `account already holds ${remote.length}/${VOCABULARY_QUOTA} hotword lists`,
      "vox vocab.prune",
    );
  }

  const id = await wrapApi(() => client.createVocabulary(model, prefixFor(vocab.name), words));
  await wrapApi(() => client.awaitVocabulary(id, DEPLOY_TIMEOUT_MS));
  indexSet(idx, vocab.name, model, {
    vocabulary_id: id,
    content_hash: hash,
    synced_at: new Date().toISOString(),
  });
  await writeIndex(voxDir, idx);
  result.vocabularyId = id;
  result.action = "created";
  return result;
}

export async function pruneVocabularies(
  client: DashScopeClient,
  voxDir: string,
  dryRun: boolean,
): Promise<string[]> {
  const idx = await loadIndex(voxDir);
  const names = new Set(await vocabNames(voxDir));
  const claimed = claimedIds(idx, names);
  const remote = await wrapApi(() => client.listVocabularies());
  const orphans: string[] = [];
  for (const info of remote) {
    if (claimed[info.id]) continue;
    orphans.push(info.id);
    if (dryRun) continue;
    await wrapApi(() => client.deleteVocabulary(info.id));
  }
  if (!dryRun && forgetUnbacked(idx, names)) await writeIndex(voxDir, idx);
  return orphans;
}

async function verifyBinding(
  client: DashScopeClient,
  vocabularyId: string,
  model: string,
): Promise<void> {
  const { info } = await client.queryVocabulary(vocabularyId);
  if (info.target_model && info.target_model !== model) {
    throw voxError(
      "vocab_model_mismatch",
      `list ${vocabularyId} was built for ${info.target_model}, not ${model} — its hotwords would be ignored`,
      `vox vocab.sync --name <name> --model ${vendorHint(model)}`,
    );
  }
  if (info.status && info.status !== "OK") {
    throw new Error(`list ${vocabularyId} is ${info.status}`);
  }
}

function vendorHint(model: string): string {
  if (model === MODEL_FUN_ASR) return "fun";
  return "qwen";
}

async function wrapApi<T>(fn: () => Promise<T>): Promise<T> {
  try {
    return await fn();
  } catch (error) {
    throw voxError("api_error", error instanceof Error ? error.message : String(error));
  }
}

function isNotFound(error: unknown): boolean {
  return typeof error === "object" && error !== null && "code" in error && error.code === "ENOENT";
}

export { tilde };
