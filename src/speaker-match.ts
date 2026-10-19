import type { Sentence } from "./dashscope.ts";

export const SPEAKER_MODEL = "wespeaker-resnet34-lm-v1";
export const EMBEDDING_DIM = 256;
export type SpeakerProfile = {
  id: string;
  name: string;
  embedding: number[];
  created: string;
};
export type SpeakerSettings = {
  model: typeof SPEAKER_MODEL;
  threshold: number;
  margin: number;
  profiles: SpeakerProfile[];
};
export type AudioSegment = { id: string; start: number; end: number };
export type SpeakerEmbedding = { id: string; vector: number[]; duration: number };
export type SpeakerMatch = {
  speaker: string;
  status: "matched" | "unknown" | "insufficient_audio" | "inconsistent";
  id?: string;
  name?: string;
  score?: number;
  margin?: number;
  segments: number;
};

export function normalized(values: number[]): number[] {
  if (values.length !== EMBEDDING_DIM || values.some((n) => !Number.isFinite(n))) {
    throw new Error(`Expected ${EMBEDDING_DIM} finite speaker embedding values`);
  }
  const norm = Math.hypot(...values);
  if (norm < 1e-8) throw new Error("Speaker embedding has zero norm");
  return values.map((n) => n / norm);
}

export function centroid(vectors: number[][]): number[] {
  if (vectors.length === 0) throw new Error("No speaker embeddings to combine");
  const unit = vectors.map(normalized);
  return normalized(unit[0]!.map((_, i) => unit.reduce((sum, v) => sum + v[i]!, 0)));
}

function cosine(a: number[], b: number[]): number {
  return a.reduce((sum, n, i) => sum + n * b[i]!, 0);
}

function rank(vector: number[], settings: SpeakerSettings) {
  const unit = normalized(vector);
  return settings.profiles
    .map((profile) => ({ profile, score: cosine(unit, normalized(profile.embedding)) }))
    .sort((a, b) => b.score - a.score || a.profile.id.localeCompare(b.profile.id));
}

export function matchSpeaker(
  speaker: string,
  vectors: number[][],
  settings: SpeakerSettings,
): SpeakerMatch {
  if (vectors.length === 0) return { speaker, status: "insufficient_audio", segments: 0 };
  const ranked = rank(centroid(vectors), settings);
  const best = ranked[0];
  if (!best) return { speaker, status: "unknown", segments: vectors.length };
  const margin = best.score - (ranked[1]?.score ?? -1);
  const base = { speaker, score: best.score, margin, segments: vectors.length };
  if (best.score < settings.threshold || margin < settings.margin) {
    return { ...base, status: "unknown" };
  }
  const votes = vectors.map((vector) => rank(vector, settings));
  // Contradictory clear matches suggest the ASR cluster merged different people.
  const conflicting = votes.some((vote) => {
    const first = vote[0]!;
    return (
      first.profile.id !== best.profile.id ||
      first.score < settings.threshold ||
      first.score - (vote[1]?.score ?? -1) < settings.margin
    );
  });
  if (conflicting) return { ...base, status: "inconsistent" };
  return { ...base, status: "matched", id: best.profile.id, name: best.profile.name };
}

/** Pick up to three clean, non-overlapping 3–10s excerpts per ASR speaker. */
export function speakerSegments(sentences: Sentence[]): AudioSegment[] {
  const valid = sentences.filter(
    (s) =>
      s.speaker !== undefined &&
      s.speaker !== "" &&
      Number.isFinite(s.begin_time) &&
      Number.isFinite(s.end_time) &&
      s.begin_time >= 0 &&
      s.end_time > s.begin_time,
  );
  const groups = new Map<string, { start: number; end: number }[]>();
  for (const s of [...valid].sort((a, b) => a.begin_time - b.begin_time)) {
    const id = s.speaker!;
    const spans = groups.get(id) ?? [];
    const last = spans.at(-1);
    if (last && s.begin_time / 1000 <= last.end + 0.25)
      last.end = Math.max(last.end, s.end_time / 1000);
    else spans.push({ start: s.begin_time / 1000, end: s.end_time / 1000 });
    groups.set(id, spans);
  }
  const result: AudioSegment[] = [];
  for (const [id, spans] of groups) {
    // Subtract other speakers rather than sampling cross-talk into a voiceprint.
    const clean: { start: number; end: number }[] = [];
    for (const span of spans) {
      let pieces = [span];
      for (const other of valid) {
        if (other.speaker === id) continue;
        const start = other.begin_time / 1000;
        const end = other.end_time / 1000;
        pieces = pieces.flatMap((p) => {
          if (end <= p.start || start >= p.end) return [p];
          return [
            { start: p.start, end: Math.min(start, p.end) },
            { start: Math.max(end, p.start), end: p.end },
          ].filter((q) => q.end > q.start);
        });
      }
      for (const piece of pieces) {
        for (let start = piece.start; start + 3 <= piece.end; start += 10) {
          clean.push({ start, end: Math.min(start + 10, piece.end) });
        }
      }
    }
    clean.sort((a, b) => b.end - b.start - (a.end - a.start) || a.start - b.start);
    for (const span of clean.slice(0, 3)) result.push({ id, ...span });
  }
  return result;
}
