import { createHash } from "node:crypto";
import { mkdir, readFile, rename, rm, writeFile } from "node:fs/promises";
import { join, resolve, relative, isAbsolute } from "node:path";

import * as v from "valibot";

import { voxError } from "./cli-error.ts";
import type { AsrResult } from "./dashscope.ts";
import { render } from "./export.ts";
import { expandHome, tilde } from "./paths.ts";
import { loadRun, newStore, resolveSid, runDir, type RunRecord } from "./run-store.ts";
import {
  centroid,
  EMBEDDING_DIM,
  matchSpeaker,
  normalized,
  speakerSegments,
  SPEAKER_MODEL,
  type SpeakerMatch,
  type SpeakerProfile,
  type SpeakerSettings,
} from "./speaker-match.ts";
import { checkSpeakerRuntime, embedAudio } from "./speaker-runtime.ts";

const finite = v.pipe(v.number(), v.finite());
const settingsSchema = v.object({
  model: v.literal(SPEAKER_MODEL),
  threshold: v.pipe(finite, v.minValue(0), v.maxValue(1)),
  margin: v.pipe(finite, v.minValue(0), v.maxValue(1)),
  profiles: v.array(
    v.object({
      id: v.pipe(v.string(), v.regex(/^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$/)),
      name: v.pipe(v.string(), v.minLength(1)),
      embedding: v.pipe(v.array(finite), v.length(EMBEDDING_DIM)),
      created: v.string(),
    }),
  ),
});

export async function loadSpeakers(home: string): Promise<SpeakerSettings> {
  let raw: string;
  try {
    raw = await readFile(join(home, "speakers.json"), "utf8");
  } catch (error) {
    if (hasCode(error, "ENOENT"))
      return { model: SPEAKER_MODEL, threshold: 0.65, margin: 0.1, profiles: [] };
    throw voxError("io_error", `Cannot read speaker profiles: ${String(error)}`);
  }
  try {
    const value = v.parse(settingsSchema, JSON.parse(raw));
    const ids = new Set(value.profiles.map((p) => p.id));
    if (ids.size !== value.profiles.length) throw new Error("Duplicate speaker IDs");
    for (const p of value.profiles) normalized(p.embedding);
    return value;
  } catch (error) {
    throw voxError("io_error", `Invalid speakers.json: ${String(error)}`);
  }
}

async function mutateSpeakers<T>(
  home: string,
  mutate: (settings: SpeakerSettings) => Promise<T>,
): Promise<T> {
  await mkdir(home, { recursive: true });
  const lock = join(home, ".speakers.lock");
  try {
    await mkdir(lock);
  } catch (error) {
    if (hasCode(error, "EEXIST"))
      throw voxError(
        "io_error",
        "Another speaker update is active",
        `If no vox process is running, remove ${lock}`,
      );
    throw error;
  }
  try {
    const settings = await loadSpeakers(home);
    const result = await mutate(settings);
    await atomicJson(join(home, "speakers.json"), settings);
    return result;
  } finally {
    await rm(lock, { recursive: true, force: true });
  }
}

function profileInfo(profile: SpeakerProfile) {
  return { id: profile.id, name: profile.name, created: profile.created };
}

export async function listSpeakers(home: string) {
  const settings = await loadSpeakers(home);
  return {
    model: settings.model,
    threshold: settings.threshold,
    margin: settings.margin,
    profiles: settings.profiles.map(profileInfo),
    path: tilde(join(home, "speakers.json")),
  };
}

export async function configureSpeakers(
  home: string,
  input: { threshold?: number; margin?: number },
) {
  if (input.threshold === undefined && input.margin === undefined) return listSpeakers(home);
  await mutateSpeakers(home, async (settings) => {
    settings.threshold = input.threshold ?? settings.threshold;
    settings.margin = input.margin ?? settings.margin;
    v.parse(settingsSchema, settings);
  });
  return listSpeakers(home);
}

export type EnrollInput = {
  id: string;
  name: string;
  file: string;
  start?: number;
  end?: number;
  replace?: boolean;
};
export async function enrollSpeaker(home: string, input: EnrollInput) {
  const id = input.id;
  if (!/^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$/.test(id) || !input.name.trim())
    throw voxError("invalid_usage", "Pass a valid speaker id and non-empty name");
  const start = input.start ?? 0;
  const end = input.end ?? start + 30;
  if (
    !Number.isFinite(start) ||
    !Number.isFinite(end) ||
    start < 0 ||
    end - start < 3 ||
    end - start > 30
  ) {
    throw voxError(
      "invalid_usage",
      "Enrollment must select 3–30 seconds of one speaker using start/end (seconds)",
    );
  }
  return mutateSpeakers(home, async (settings) => {
    const existing = settings.profiles.find((p) => p.id === id);
    if (existing && !input.replace)
      throw voxError(
        "invalid_usage",
        `Speaker ${id} already exists`,
        "Pass replace: true to re-enroll this speaker",
      );
    const embeddings = await embedAudio(home, input.file, [{ id, start, end }]);
    if (embeddings.length === 0)
      throw voxError("audio_unsupported", "No usable enrollment audio (need at least 3 seconds)");
    const profile: SpeakerProfile = {
      id,
      name: input.name.trim(),
      embedding: centroid(embeddings.map((e) => e.vector)),
      created: new Date().toISOString(),
    };
    settings.profiles = [...settings.profiles.filter((p) => p.id !== id), profile].sort((a, b) =>
      a.id.localeCompare(b.id),
    );
    return {
      enrolled: profileInfo(profile),
      model: settings.model,
      seconds: embeddings.reduce((n, e) => n + e.duration, 0),
    };
  });
}

export async function removeSpeaker(home: string, id: string) {
  return mutateSpeakers(home, async (settings) => {
    const found = settings.profiles.some((p) => p.id === id);
    settings.profiles = settings.profiles.filter((p) => p.id !== id);
    return { removed: found ? id : null };
  });
}

export async function identificationSettings(
  home: string,
  ids?: string[],
): Promise<SpeakerSettings> {
  const settings = await loadSpeakers(home);
  if (ids) {
    if (ids.length === 0)
      throw voxError("invalid_usage", "speakerIds must contain at least one enrolled ID");
    for (const id of ids)
      if (!settings.profiles.some((p) => p.id === id))
        throw voxError("speaker_not_found", `Speaker ${id} is not enrolled`, "vox speaker.list");
    settings.profiles = settings.profiles.filter((p) => ids.includes(p.id));
  }
  if (!settings.profiles.length)
    throw voxError("speaker_not_found", "No speakers are enrolled", "vox speaker.enroll");
  checkSpeakerRuntime();
  return settings;
}

export type Identification = {
  model: string;
  profilesHash: string;
  threshold: number;
  margin: number;
  created: string;
  matches: SpeakerMatch[];
};

export async function identifyResult(
  home: string,
  audio: string,
  result: AsrResult,
  settings: SpeakerSettings,
): Promise<AsrResult> {
  const ids = [
    ...new Set(
      (result.sentences ?? []).flatMap((s) =>
        s.speaker === undefined || s.speaker === "" ? [] : [s.speaker],
      ),
    ),
  ];
  if (!ids.length)
    throw voxError(
      "speaker_labels_missing",
      "ASR returned no speaker labels",
      "Re-run hear with speakers: true using fun or qwen",
    );
  const spans = speakerSegments(result.sentences ?? []);
  const embeddings = spans.length ? await embedAudio(home, audio, spans) : [];
  const matches = ids.map((id) =>
    matchSpeaker(
      id,
      embeddings.filter((e) => e.id === id).map((e) => e.vector),
      settings,
    ),
  );
  const byId = new Map(matches.map((m) => [m.speaker, m]));
  return {
    ...result,
    identification: {
      model: settings.model,
      profilesHash: createHash("sha256").update(JSON.stringify(settings)).digest("hex"),
      threshold: settings.threshold,
      margin: settings.margin,
      created: new Date().toISOString(),
      matches,
    },
    sentences: result.sentences?.map((s) => {
      const match = s.speaker === undefined ? undefined : byId.get(s.speaker);
      return { ...s, speaker_name: match?.name, speaker_identity: match?.id };
    }),
  };
}

/** Save identity as a projection; ASR cache and anonymous speaker IDs remain intact. */
export async function writeTranscript(
  home: string,
  rec: RunRecord,
  output?: string,
  identified = false,
) {
  const dir = runDir(newStore(home), rec.meta.sid);
  const path = output ? resolve(expandHome(output)) : join(dir, "transcript.md");
  // Do not permit a transcript destination to overwrite the source audio or run state.
  const source = resolve(expandHome(rec.meta.source));
  const reserved = [
    source,
    join(dir, "run.json"),
    join(dir, "meta.yaml"),
    join(dir, "identified.json"),
    join(dir, `audio.${rec.args.format}`),
    join(home, "speakers.json"),
    join(home, "config.json"),
  ].map((p) => resolve(p));
  const internal = relative(resolve(home), path);
  const insideHome = internal === "" || (!internal.startsWith("..") && !isAbsolute(internal));
  if (reserved.includes(path) || (insideHome && path !== join(dir, "transcript.md")))
    throw voxError("invalid_usage", "Transcript output would overwrite source audio or vox state");
  await mkdir(dir, { recursive: true });
  await atomicWrite(path, render(rec, "md"));
  if (identified) await atomicJson(join(dir, "identified.json"), rec);
  return {
    transcript: tilde(path),
    ...(identified ? { speakers: rec.result.identification?.matches } : {}),
  };
}

export async function identifyRun(
  home: string,
  input: { sid: string; speakerIds?: string[]; output?: string },
) {
  const settings = await identificationSettings(home, input.speakerIds);
  const store = newStore(home);
  const sid = await resolveSid(store, input.sid);
  const rec = await loadRun(store, sid);
  const audio = join(runDir(store, sid), `audio.${rec.args.format}`);
  const result = await identifyResult(home, audio, rec.result, settings);
  return { sid, ...(await writeTranscript(home, { ...rec, result }, input.output, true)) };
}

export async function loadIdentifiedRun(home: string, rec: RunRecord): Promise<RunRecord> {
  const path = join(runDir(newStore(home), rec.meta.sid), "identified.json");
  let raw;
  try {
    raw = await readFile(path, "utf8");
  } catch (error) {
    if (hasCode(error, "ENOENT")) return rec;
    throw error;
  }
  try {
    const projection = JSON.parse(raw) as RunRecord;
    if (
      projection.digest !== rec.digest ||
      !projection.result?.identification ||
      !Array.isArray(projection.result.sentences)
    )
      throw new Error("Invalid identity projection");
    return projection;
  } catch (error) {
    throw voxError("io_error", `Invalid identified transcript: ${String(error)}`);
  }
}

async function atomicWrite(path: string, text: string): Promise<void> {
  const tmp = `${path}.${crypto.randomUUID()}.tmp`;
  try {
    await writeFile(tmp, text, { mode: 0o600 });
    await rename(tmp, path);
  } catch (error) {
    throw voxError("io_error", `Cannot write ${path}: ${String(error)}`);
  } finally {
    await rm(tmp, { force: true });
  }
}
async function atomicJson(path: string, value: unknown) {
  await atomicWrite(path, JSON.stringify(value, null, 2) + "\n");
}
function hasCode(error: unknown, code: string) {
  return typeof error === "object" && error !== null && "code" in error && error.code === code;
}
