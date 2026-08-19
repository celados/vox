import { expect, test } from "bun:test";
import { parse } from "yaml";

import type { AsrWord } from "./dashscope.ts";
import {
  MAX_CUE_RUNES,
  render,
  renderMarkdown,
  renderVtt,
  segment,
  srtTime,
  vttTime,
} from "./export.ts";
import type { RunRecord } from "./run-store.ts";

function word(text: string, punct: string, begin: number, end: number): AsrWord {
  return { text, punctuation: punct, begin_time: begin, end_time: end };
}

test("segment splits on terminator", () => {
  const cues = segment([
    word("你好", "", 0, 500),
    word("世界", "。", 500, 1000),
    word("再见", "。", 1200, 1700),
  ]);
  expect(cues).toHaveLength(2);
  expect(cues[0]?.text).toBe("你好世界。");
  expect(cues[0]?.beginMs).toBe(0);
  expect(cues[0]?.endMs).toBe(1000);
  expect(cues[1]?.index).toBe(2);
});

test("segment splits on gap", () => {
  const cues = segment([word("前半句", "", 0, 500), word("后半句", "", 1500, 2000)]);
  expect(cues).toHaveLength(2);
});

test("segment keeps short clauses together", () => {
  const cues = segment([word("好", "，", 0, 200), word("的", "。", 200, 400)]);
  expect(cues).toHaveLength(1);
});

test("segment empty", () => {
  expect(segment([])).toHaveLength(0);
});

test("time formats", () => {
  const ms = (1 * 3600 + 2 * 60 + 3) * 1000 + 45;
  expect(srtTime(ms)).toBe("01:02:03,045");
  expect(vttTime(ms)).toBe("01:02:03.045");
});

test("render VTT header", () => {
  expect(
    renderVtt([{ index: 1, beginMs: 0, endMs: 1000, text: "x" }]).startsWith("WEBVTT\n\n"),
  ).toBe(true);
});

test("renders without word timings", () => {
  const rec: RunRecord = {
    digest: "d",
    meta: {
      sid: "abc",
      source: "x.wav",
      model: "m",
      created: "2026-08-01T00:00:00Z",
      path: "p",
      size: { tokens: 1, words: 1, chars: 1, duration: 12 },
    },
    args: { model: "m", format: "wav" },
    result: { text: "整段文本，没有词级时间戳。" },
  };
  const srt = render(rec, "srt");
  expect(srt).toContain("整段文本");
  expect(srt).toContain("00:00:12,000");
  expect(render(rec, "md")).toContain("整段文本");
});

test("segment repairs timeline", () => {
  const cues = segment([
    word("后", "。", 1000, 1500),
    word("先", "。", 500, 700),
    word("零", "。", 2000, 2000),
  ]);
  let prev = -1;
  for (const cue of cues) {
    expect(cue.endMs).toBeGreaterThanOrEqual(cue.beginMs);
    expect(cue.endMs).not.toBe(cue.beginMs);
    expect(cue.beginMs).toBeGreaterThanOrEqual(prev);
    prev = cue.endMs;
  }
});

test("segment keeps cues within budget", () => {
  const words: AsrWord[] = [];
  for (let i = 0; i < 40; i++) words.push(word("字", "", i * 100, i * 100 + 100));
  for (const cue of segment(words)) {
    expect([...cue.text].length).toBeLessThanOrEqual(MAX_CUE_RUNES);
  }
});

test("markdown frontmatter is valid YAML", () => {
  const rec: RunRecord = {
    digest: "d",
    meta: {
      sid: "abc",
      source: "/tmp/a: b #1.wav",
      model: "m",
      created: "2026-08-01T00:00:00Z",
      path: "p",
      size: { tokens: 1, words: 1, chars: 1, duration: 1 },
    },
    args: { model: "m", format: "wav" },
    result: { text: "x。" },
  };
  const md = renderMarkdown(rec);
  const stripped = md.replace(/^---\n/, "");
  const cut = stripped.indexOf("---\n");
  expect(cut).toBeGreaterThan(0);
  const head = stripped.slice(0, cut);
  const body = stripped.slice(cut + 4);
  const parsed = parse(head) as Record<string, unknown>;
  expect(parsed.source).toBe("/tmp/a: b #1.wav");
  expect(body).toContain("x。");
});
