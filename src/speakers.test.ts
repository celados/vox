import { expect, test } from "bun:test";
import { mkdtemp, readFile, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  configureSpeakers,
  identificationSettings,
  listSpeakers,
  loadIdentifiedRun,
  loadSpeakers,
  removeSpeaker,
  writeTranscript,
} from "./speakers.ts";
import { SPEAKER_MODEL } from "./speaker-match.ts";
import { newStore, saveRun, type RunRecord } from "./run-store.ts";
import { render } from "./export.ts";

function record(): RunRecord {
  return {
    digest: "abcd",
    meta: {
      sid: "abcdef123456",
      source: "/tmp/input.wav",
      model: "fun-asr",
      created: "2026-10-16T00:00:00Z",
      path: "p",
      size: { tokens: 1, words: 0, chars: 5, duration: 12 },
    },
    args: { model: "fun-asr", format: "wav", speakers: true },
    result: {
      text: "Hello.你好。",
      sentences: [
        {
          speaker: "0",
          speaker_name: "Ethan",
          speaker_identity: "ethan",
          begin_time: 0,
          end_time: 8000,
          text: "A long sentence without word timings that must never disappear.",
        },
        { speaker: "1", begin_time: 8000, end_time: 12000, text: "你好。" },
      ],
      identification: {
        model: SPEAKER_MODEL,
        profilesHash: "test",
        threshold: 0.65,
        margin: 0.1,
        created: "now",
        matches: [{ speaker: "0", id: "ethan", name: "Ethan", status: "matched", segments: 1 }],
      },
    },
  };
}

test("speaker settings persist atomically, reject corruption, and do not expose vectors", async () => {
  const home = await mkdtemp(join(tmpdir(), "vox-speaker-"));
  await configureSpeakers(home, { threshold: 0.7 });
  expect((await loadSpeakers(home)).threshold).toBe(0.7);
  expect((await listSpeakers(home)).profiles).toEqual([]);
  expect(await removeSpeaker(home, "absent")).toEqual({ removed: null });
  await expect(identificationSettings(home)).rejects.toMatchObject({ code: "speaker_not_found" });
  await writeFile(join(home, "speakers.json"), '{"model":"wrong"}');
  await expect(loadSpeakers(home)).rejects.toMatchObject({ code: "io_error" });
});

test("named transcript preserves anonymous IDs and original ASR, and all formats retain text", async () => {
  const home = await mkdtemp(join(tmpdir(), "vox-transcript-"));
  const rec = record();
  await saveRun(newStore(home), rec, new Uint8Array([0]), "wav");
  const out = await writeTranscript(
    home,
    rec,
    join(home, "runs", rec.meta.sid, "transcript.md"),
    true,
  );
  const md = await readFile(out.transcript, "utf8");
  expect(md).toContain("[00:00:00] [Ethan]");
  expect(md).toContain("[Speaker 1] 你好。");
  expect((await loadIdentifiedRun(home, rec)).result.sentences?.[0]?.speaker).toBe("0");
  for (const format of ["md", "srt", "vtt", "txt"]) {
    const text = render(rec, format);
    expect(text).toContain("[Ethan]");
    expect(text).toContain("must never disappear.");
  }
  const raw = JSON.parse(await readFile(join(home, "runs", rec.meta.sid, "run.json"), "utf8"));
  expect(raw.digest).toBe("abcd");
  await expect(writeTranscript(home, rec, join(home, "state.json"))).rejects.toMatchObject({
    code: "invalid_usage",
  });
  await expect(writeTranscript(home, rec, join(home, "config.json"))).rejects.toMatchObject({
    code: "invalid_usage",
  });
});
