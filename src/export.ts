import { stringify } from "yaml";

import type { AsrWord, Sentence } from "./dashscope.ts";
import type { RunRecord } from "./run-store.ts";

export const MAX_CUE_RUNES = 28;
export const MAX_CUE_DURATION_MS = 6_000;
export const GAP_SPLIT_MS = 400;
export const MIN_WORD_MS = 40;

const TERMINATORS = "。！？!?…";
const SEPARATORS = "，、；：,;:";

export type Cue = {
  index: number;
  beginMs: number;
  endMs: number;
  text: string;
};

export function segment(words: AsrWord[]): Cue[] {
  const sanitized = sanitize(words);
  const cues: Cue[] = [];
  let buf = "";
  let begin = 0;
  let end = 0;
  let runeCount = 0;
  let started = false;

  const flush = () => {
    const text = buf.trim();
    buf = "";
    runeCount = 0;
    started = false;
    if (!text) return;
    cues.push({ index: cues.length + 1, beginMs: begin, endMs: end, text });
  };

  for (let i = 0; i < sanitized.length; i++) {
    const word = sanitized[i]!;
    const wordBegin = word.begin_time;
    const wordEnd = word.end_time;
    if (started && wordBegin - end >= GAP_SPLIT_MS) flush();
    if (
      started &&
      (runeCount + [...word.text].length > MAX_CUE_RUNES || wordEnd - begin > MAX_CUE_DURATION_MS)
    ) {
      flush();
    }
    if (!started) {
      begin = wordBegin;
      started = true;
    }
    buf += word.text + word.punctuation;
    runeCount += [...word.text].length;
    end = wordEnd;

    const last = i === sanitized.length - 1;
    if (last) flush();
    else if (containsAny(word.punctuation, TERMINATORS)) flush();
    else if (containsAny(word.punctuation, SEPARATORS) && runeCount >= MAX_CUE_RUNES / 2) flush();
    else if (runeCount >= MAX_CUE_RUNES) flush();
    else if (end - begin >= MAX_CUE_DURATION_MS) flush();
  }
  if (started) flush();
  return cues;
}

function sanitize(words: AsrWord[]): AsrWord[] {
  const out: AsrWord[] = [];
  let prevEnd = 0;
  for (const word of words) {
    if (!word.text.trim() && !word.punctuation) continue;
    const next = { ...word };
    if (next.begin_time < prevEnd) next.begin_time = prevEnd;
    if (next.end_time <= next.begin_time) next.end_time = next.begin_time + MIN_WORD_MS;
    prevEnd = next.end_time;
    out.push(next);
  }
  return out;
}

export function render(rec: RunRecord, format: string): string {
  switch (format) {
    case "txt":
      return rec.result.text + "\n";
    case "json":
      return JSON.stringify(rec, null, 2) + "\n";
    case "md":
      return renderMarkdown(rec);
    case "srt":
      return renderSrt(cuesOf(rec));
    case "vtt":
      return renderVtt(cuesOf(rec));
    default:
      throw new Error(`unknown format "${format}"`);
  }
}

export function cuesOf(rec: RunRecord): Cue[] {
  if (rec.result.sentences && rec.result.sentences.length > 0) {
    return fromSentences(rec.result.sentences);
  }
  if (!rec.result.text.trim()) return [];
  return [
    {
      index: 1,
      beginMs: 0,
      endMs: rec.meta.size.duration * 1000,
      text: rec.result.text,
    },
  ];
}

function fromSentences(sentences: Sentence[]): Cue[] {
  const cues: Cue[] = [];
  for (const sentence of sentences) {
    let text = sentence.text.trim();
    if (!text) continue;
    if (sentence.speaker) text = `[${sentence.speaker}] ${text}`;
    const duration = sentence.end_time - sentence.begin_time;
    if ([...text].length <= MAX_CUE_RUNES && duration <= MAX_CUE_DURATION_MS) {
      cues.push({
        index: cues.length + 1,
        beginMs: sentence.begin_time,
        endMs: sentence.end_time,
        text,
      });
      continue;
    }
    let first = true;
    for (const cue of segment(sentence.words ?? [])) {
      const next = { ...cue, index: cues.length + 1 };
      // Prefix the first split cue of this sentence, not of the whole file.
      if (sentence.speaker && first) {
        next.text = `[${sentence.speaker}] ${next.text}`;
        first = false;
      }
      cues.push(next);
    }
  }
  return cues;
}

export function renderMarkdown(rec: RunRecord): string {
  const head = stringify({
    type: "Transcript",
    sid: rec.meta.sid,
    source: rec.meta.source,
    model: rec.meta.model,
    ...(rec.meta.vocab ? { vocab: rec.meta.vocab } : {}),
    duration: formatDuration(rec.meta.size.duration * 1000),
    generated: { by: "vox", at: toRfc3339(rec.meta.created) },
  });

  let body = "";
  const units = cuesOf(rec);
  if (units.length === 0) {
    body = rec.result.text.trim() + "\n";
    return `---\n${head}---\n\n${body}`;
  }

  let para = "";
  for (const cue of units) {
    para += cue.text;
    if (containsAny(lastRune(cue.text), TERMINATORS)) {
      body += para + "\n\n";
      para = "";
    }
  }
  if (para) body += para + "\n";
  return `---\n${head}---\n\n${body}`;
}

export function renderSrt(cues: Cue[]): string {
  let out = "";
  for (const cue of cues) {
    out += `${cue.index}\n${srtTime(cue.beginMs)} --> ${srtTime(cue.endMs)}\n${cue.text}\n\n`;
  }
  return out;
}

export function renderVtt(cues: Cue[]): string {
  let out = "WEBVTT\n\n";
  for (const cue of cues) {
    out += `${vttTime(cue.beginMs)} --> ${vttTime(cue.endMs)}\n${cue.text}\n\n`;
  }
  return out;
}

export function srtTime(ms: number): string {
  const parts = splitMs(ms);
  return `${pad(parts.h, 2)}:${pad(parts.m, 2)}:${pad(parts.s, 2)},${pad(parts.ms, 3)}`;
}

export function vttTime(ms: number): string {
  const parts = splitMs(ms);
  return `${pad(parts.h, 2)}:${pad(parts.m, 2)}:${pad(parts.s, 2)}.${pad(parts.ms, 3)}`;
}

function formatDuration(ms: number): string {
  const parts = splitMs(ms);
  if (parts.h > 0) return `${pad(parts.h, 2)}:${pad(parts.m, 2)}:${pad(parts.s, 2)}`;
  return `${pad(parts.m, 2)}:${pad(parts.s, 2)}`;
}

function splitMs(ms: number): { h: number; m: number; s: number; ms: number } {
  const total = Math.trunc(ms);
  return {
    h: Math.floor(total / 3_600_000),
    m: Math.floor(total / 60_000) % 60,
    s: Math.floor(total / 1000) % 60,
    ms: total % 1000,
  };
}

function pad(n: number, width: number): string {
  return String(n).padStart(width, "0");
}

function lastRune(text: string): string {
  const chars = [...text];
  return chars[chars.length - 1] ?? "";
}

function containsAny(text: string, chars: string): boolean {
  for (const ch of text) {
    if (chars.includes(ch)) return true;
  }
  return false;
}

function toRfc3339(created: string): string {
  const date = new Date(created);
  if (Number.isNaN(date.getTime())) return created;
  return date.toISOString();
}
