import { expect, test } from "bun:test";
import { mkdir, mkdtemp, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { MODEL_FUN_ASR, MODEL_QWEN_AUDIO_FILE } from "./dashscope.ts";
import { VoxError } from "./cli-error.ts";
import {
  claimedIds,
  contentHash,
  forgetUnbacked,
  loadIndex,
  loadVocabulary,
  prefixFor,
  resolveWords,
  SUPER_WEIGHT,
  syncVocabulary,
  vocabDir,
  vocabPath,
  type Index,
  type Vocabulary,
} from "./vocab.ts";
import type { DashScopeClient } from "./dashscope.ts";

function weight(n: number): number {
  return n;
}

function fixture(): Vocabulary {
  return {
    name: "meeting",
    path: "meeting.yaml",
    file: {
      lang: "zh",
      default_weight: 4,
      words: { 百炼: weight(5), 赛德克巴莱: null },
    },
  };
}

test("resolve applies default weight", () => {
  const { words } = resolveWords(fixture(), MODEL_FUN_ASR);
  const byText = Object.fromEntries(words.map((word) => [word.text, word.weight]));
  expect(byText["百炼"]).toBe(5);
  expect(byText["赛德克巴莱"]).toBe(4);
});

test("resolve clamps super weight on Fun-ASR", () => {
  const vocab = fixture();
  vocab.file.words["Qwen"] = SUPER_WEIGHT;
  const fun = resolveWords(vocab, MODEL_FUN_ASR);
  expect(fun.words.find((word) => word.text === "Qwen")?.weight).toBe(5);
  expect(fun.warnings.length).toBeGreaterThan(0);

  const qwen = resolveWords(vocab, MODEL_QWEN_AUDIO_FILE);
  expect(qwen.words.find((word) => word.text === "Qwen")?.weight).toBe(SUPER_WEIGHT);
});

test("resolve model block overrides base", () => {
  const vocab = fixture();
  vocab.file.models = {
    [MODEL_FUN_ASR]: { words: { 百炼: 2, 声网: 5 } },
  };
  const fun = resolveWords(vocab, MODEL_FUN_ASR);
  const byText = Object.fromEntries(fun.words.map((word) => [word.text, word.weight]));
  expect(byText["百炼"]).toBe(2);
  expect(byText["声网"]).toBe(5);

  const qwen = resolveWords(vocab, MODEL_QWEN_AUDIO_FILE);
  expect(qwen.words.some((word) => word.text === "声网")).toBe(false);
});

test("resolve drops oversized words", () => {
  const vocab = fixture();
  vocab.file.words["这是一个非常长的热词超过了十五个字符的限制"] = 4;
  vocab.file.words["one two three four five six seven eight"] = 4;
  const { words, warnings } = resolveWords(vocab, MODEL_FUN_ASR);
  expect(words.some((word) => [...word.text].length > 15)).toBe(false);
  expect(warnings.length).toBeGreaterThan(0);
});

test("resolve drops unsupported lang on Fun-ASR", () => {
  const vocab = fixture();
  vocab.file.lang = "de";
  const fun = resolveWords(vocab, MODEL_FUN_ASR);
  expect(fun.words.every((word) => !word.lang)).toBe(true);
  expect(fun.warnings.length).toBeGreaterThan(0);

  const qwen = resolveWords(vocab, MODEL_QWEN_AUDIO_FILE);
  expect(qwen.words[0]?.lang).toBe("de");
});

test("content hash is stable and content-sensitive", () => {
  const first = contentHash(resolveWords(fixture(), MODEL_FUN_ASR).words);
  for (let i = 0; i < 20; i++) {
    expect(contentHash(resolveWords(fixture(), MODEL_FUN_ASR).words)).toBe(first);
  }
  const edited = fixture();
  edited.file.words["百炼"] = 3;
  expect(contentHash(resolveWords(edited, MODEL_FUN_ASR).words)).not.toBe(first);
});

test("prefixFor matches the API constraint", () => {
  expect(prefixFor("meeting")).toBe("meeting");
  expect(prefixFor("Dev-Notes_2026")).toBe("devnotes20");
  expect(prefixFor("中文")).toBe("vox");
  expect(prefixFor("averyveryverylongname")).toBe("averyveryv");
});

test("claimed ids ignore deleted YAML", async () => {
  const dir = await mkdtemp(join(tmpdir(), "vox-vocab-"));
  await mkdir(vocabDir(dir), { recursive: true });
  await writeFile(vocabPath(dir, "kept"), "words: {a: 4}\n");
  const idx: Index = {
    kept: { [MODEL_FUN_ASR]: { vocabulary_id: "vocab-kept", content_hash: "x", synced_at: "" } },
    deleted: {
      [MODEL_FUN_ASR]: { vocabulary_id: "vocab-deleted", content_hash: "x", synced_at: "" },
    },
  };
  const existing = new Set(["kept"]);
  const claimed = claimedIds(idx, existing);
  expect(claimed["vocab-kept"]).toBe(true);
  expect(claimed["vocab-deleted"]).toBeUndefined();
  expect(forgetUnbacked(idx, existing)).toBe(true);
  expect(idx.deleted).toBeUndefined();
  expect(idx.kept).toBeDefined();
});

test("loadIndex fails closed on corruption", async () => {
  const dir = await mkdtemp(join(tmpdir(), "vox-vocab-"));
  await mkdir(vocabDir(dir), { recursive: true });
  await writeFile(join(vocabDir(dir), ".index.json"), "{not json");
  await expect(loadIndex(dir)).rejects.toMatchObject({ code: "vocab_index_corrupt" });
});

test("loadIndex fails closed on valid JSON with the wrong shape", async () => {
  const shapes = ["[]", '"oops"', "123", '{"name":"not-an-object"}', '{"name":{"fun-asr":"nope"}}'];
  for (const content of shapes) {
    const dir = await mkdtemp(join(tmpdir(), "vox-vocab-"));
    await mkdir(vocabDir(dir), { recursive: true });
    await writeFile(join(vocabDir(dir), ".index.json"), content);
    await expect(loadIndex(dir), content).rejects.toMatchObject({ code: "vocab_index_corrupt" });
  }
});

test("syncVocabulary does not recreate when update fails", async () => {
  const dir = await mkdtemp(join(tmpdir(), "vox-vocab-"));
  await mkdir(vocabDir(dir), { recursive: true });
  const vocab = fixture();
  const idx: Index = {
    meeting: {
      [MODEL_FUN_ASR]: { vocabulary_id: "vocab-kept", content_hash: "stale", synced_at: "" },
    },
  };
  await writeFile(join(vocabDir(dir), ".index.json"), JSON.stringify(idx));

  let created = false;
  const client = {
    queryVocabulary: async () => ({
      info: { id: "vocab-kept", status: "OK", target_model: MODEL_FUN_ASR },
      words: [],
    }),
    updateVocabulary: async () => {
      throw new Error("HTTP 500: boom");
    },
    createVocabulary: async () => {
      created = true;
      return "vocab-new";
    },
    awaitVocabulary: async () => {},
    listVocabularies: async () => [],
  } as unknown as DashScopeClient;

  await expect(syncVocabulary(client, dir, vocab, MODEL_FUN_ASR, false)).rejects.toMatchObject({
    code: "api_error",
  });
  expect(created).toBe(false);
});

test("syncVocabulary recreates when the stored binding is gone", async () => {
  const dir = await mkdtemp(join(tmpdir(), "vox-vocab-"));
  await mkdir(vocabDir(dir), { recursive: true });
  const vocab = fixture();
  const idx: Index = {
    meeting: {
      [MODEL_FUN_ASR]: { vocabulary_id: "vocab-gone", content_hash: "x", synced_at: "" },
    },
  };
  await writeFile(join(vocabDir(dir), ".index.json"), JSON.stringify(idx));

  const client = {
    queryVocabulary: async () => {
      throw new Error("HTTP 404: missing");
    },
    createVocabulary: async () => "vocab-new",
    awaitVocabulary: async () => {},
    listVocabularies: async () => [],
    updateVocabulary: async () => {
      throw new Error("should not update");
    },
  } as unknown as DashScopeClient;

  const result = await syncVocabulary(client, dir, vocab, MODEL_FUN_ASR, false);
  expect(result.action).toBe("created");
  expect(result.vocabularyId).toBe("vocab-new");
  expect(result.warnings.some((warning) => warning.includes("gone"))).toBe(true);
});

test("load rejects unknown fields", async () => {
  const dir = await mkdtemp(join(tmpdir(), "vox-vocab-"));
  await mkdir(vocabDir(dir), { recursive: true });
  await writeFile(vocabPath(dir, "typo"), "word:\n  百炼: 5\n");
  await expect(loadVocabulary(dir, "typo")).rejects.toBeInstanceOf(VoxError);
});

test("load rejects invalid default_weight", async () => {
  const dir = await mkdtemp(join(tmpdir(), "vox-vocab-"));
  await mkdir(vocabDir(dir), { recursive: true });
  await writeFile(vocabPath(dir, "bad"), "default_weight: 99\nwords:\n  x: \n");
  await expect(loadVocabulary(dir, "bad")).rejects.toBeInstanceOf(VoxError);
});

test("resolve drops blank words", () => {
  const vocab = fixture();
  vocab.file.words["   "] = 4;
  const { words } = resolveWords(vocab, MODEL_FUN_ASR);
  expect(words.every((word) => word.text.trim() !== "")).toBe(true);
});
