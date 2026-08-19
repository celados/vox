import { expect, test } from "bun:test";
import { mkdir, mkdtemp, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { MODEL_FUN_ASR, MODEL_QWEN_AUDIO_FILE } from "./dashscope.ts";
import { VoxError } from "./cli-error.ts";
import {
  canonicalArgsJson,
  digestOf,
  listRuns,
  loadRun,
  measure,
  newStore,
  preview,
  resolveSid,
  saveRun,
  sidOf,
  type Args,
  type RunRecord,
} from "./run-store.ts";

const audio = new TextEncoder().encode("fake audio bytes");

test("canonical args JSON matches Go encoding/json omitempty and field order", () => {
  expect(canonicalArgsJson({ model: MODEL_FUN_ASR, format: "", lang: ["zh"] })).toBe(
    '{"model":"fun-asr","format":"","lang":["zh"]}',
  );
  expect(canonicalArgsJson({ model: MODEL_FUN_ASR, format: "wav" })).toBe(
    '{"model":"fun-asr","format":"wav"}',
  );
  expect(
    canonicalArgsJson({
      model: MODEL_FUN_ASR,
      format: "mp3",
      speakers: true,
      vocab: "abcd1234",
      lang: ["zh"],
    }),
  ).toBe('{"model":"fun-asr","format":"mp3","speakers":true,"vocab":"abcd1234","lang":["zh"]}');
});

test("sid is stable and arg-sensitive", () => {
  const base: Args = { model: MODEL_FUN_ASR, format: "", lang: ["zh"] };
  const first = sidOf(digestOf(audio, base));
  expect(first).toBe(sidOf(digestOf(audio, base)));
  expect(first).toHaveLength(12);
  expect(first).toBe("89b024754e68");

  const variants: Record<string, Args> = {
    model: { model: MODEL_QWEN_AUDIO_FILE, format: "", lang: ["zh"] },
    lang: { model: MODEL_FUN_ASR, format: "", lang: ["en"] },
    vocab: { model: MODEL_FUN_ASR, format: "", lang: ["zh"], vocab: "abcd1234" },
    speakers: { model: MODEL_FUN_ASR, format: "", lang: ["zh"], speakers: true },
    format: { model: MODEL_FUN_ASR, format: "mp3", lang: ["zh"] },
  };
  for (const [name, args] of Object.entries(variants)) {
    expect(sidOf(digestOf(audio, args)), name).not.toBe(first);
  }
  expect(sidOf(digestOf(new TextEncoder().encode("different audio"), base))).not.toBe(first);
});

test("measure counts CJK and Latin", () => {
  const size = measure(
    {
      text: "你好world",
      sentences: [
        {
          begin_time: 0,
          end_time: 1,
          text: "你好world",
          words: [
            { text: "你好", begin_time: 0, end_time: 1, punctuation: "" },
            { text: "world", begin_time: 1, end_time: 2, punctuation: "" },
          ],
        },
      ],
    },
    42,
  );
  expect(size.tokens).toBe(4);
  expect(size.words).toBe(2);
  expect(size.chars).toBe(7);
  expect(size.duration).toBe(42);
});

test("preview truncates on runes", () => {
  expect(preview("你好世界", 10)).toBe("你好世界");
  expect(preview("你好世界", 2)).toBe("你好…");
});

test("resolve rejects ambiguous prefix, missing prefix, and path traversal", async () => {
  const store = newStore(await mkdtemp(join(tmpdir(), "vox-store-")));
  await mkdir(join(store.dir, "abc111111111"), { recursive: true });
  await mkdir(join(store.dir, "abc222222222"), { recursive: true });

  await expect(resolveSid(store, "abc")).rejects.toBeInstanceOf(VoxError);
  expect(await resolveSid(store, "abc1")).toBe("abc111111111");
  await expect(resolveSid(store, "zzz")).rejects.toBeInstanceOf(VoxError);

  for (const prefix of ["..", "../..", "../../..", "/etc", "a/b", ".", "ABCDEF", "zzz"]) {
    await expect(resolveSid(store, prefix)).rejects.toBeInstanceOf(VoxError);
  }
});

test("load rejects digest mismatch", async () => {
  const store = newStore(await mkdtemp(join(tmpdir(), "vox-store-")));
  const rec: RunRecord = {
    digest: "digest-of-input-A",
    meta: {
      sid: "aaaaaaaaaaaa",
      source: "a.wav",
      model: MODEL_FUN_ASR,
      created: new Date().toISOString(),
      path: "~/.vox/runs/aaaaaaaaaaaa",
      size: { tokens: 1, words: 1, chars: 1, duration: 1 },
    },
    args: { model: MODEL_FUN_ASR, format: "wav" },
    result: { text: "transcript A" },
  };
  await saveRun(store, rec, audio, "wav");

  await expect(loadRun(store, "aaaaaaaaaaaa", "digest-of-input-B")).rejects.toThrow();
  const got = await loadRun(store, "aaaaaaaaaaaa", "digest-of-input-A");
  expect(got.result.text).toBe("transcript A");
});

test("save is atomic and list returns empty slice not null", async () => {
  const store = newStore(await mkdtemp(join(tmpdir(), "vox-store-")));
  const empty = await listRuns(store, "", 0);
  expect(empty).toEqual([]);

  const rec: RunRecord = {
    digest: "d",
    meta: {
      sid: "bbbbbbbbbbbb",
      source: "b.wav",
      model: MODEL_FUN_ASR,
      created: "2026-08-01T00:00:00.000Z",
      path: "x",
      size: { tokens: 0, words: 0, chars: 0, duration: 0 },
    },
    args: { model: MODEL_FUN_ASR, format: "wav" },
    result: { text: "x" },
  };
  await saveRun(store, rec, audio, "wav");
  const metas = await listRuns(store, "", 0);
  expect(metas).toHaveLength(1);
  expect(metas[0]?.sid).toBe("bbbbbbbbbbbb");
});
