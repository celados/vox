import { expect, test } from "bun:test";
import {
  matchSpeaker,
  normalized,
  speakerSegments,
  SPEAKER_MODEL,
  type SpeakerSettings,
} from "./speaker-match.ts";

function vector(index: number) {
  const v = Array<number>(256).fill(0);
  v[index] = 1;
  return v;
}
const settings: SpeakerSettings = {
  model: SPEAKER_MODEL,
  threshold: 0.65,
  margin: 0.1,
  profiles: [
    { id: "a", name: "Alice", embedding: vector(0), created: "now" },
    { id: "b", name: "Bob", embedding: vector(1), created: "now" },
  ],
};

test("identity matching rejects strangers and ambiguous candidates", () => {
  expect(matchSpeaker("0", [vector(0)], settings)).toMatchObject({
    status: "matched",
    id: "a",
    speaker: "0",
  });
  expect(matchSpeaker("1", [vector(2)], settings).status).toBe("unknown");
  expect(
    matchSpeaker("2", [normalized(vector(0).map((n, i) => n + vector(1)[i]!))], settings).status,
  ).toBe("unknown");
  expect(matchSpeaker("3", [], settings).status).toBe("insufficient_audio");
});

test("contradictory clean excerpts do not give a merged ASR cluster one name", () => {
  expect(matchSpeaker("0", [vector(0), vector(0), vector(1)], settings).status).toBe(
    "inconsistent",
  );
});

test("multiple ASR labels may identify the same person", () => {
  expect(["0", "4"].map((id) => matchSpeaker(id, [vector(0)], settings).id)).toEqual(["a", "a"]);
});

test("sample selection joins short turns and removes overlap", () => {
  const sentences = [
    { speaker: "0", begin_time: 0, end_time: 2000, text: "a" },
    { speaker: "0", begin_time: 2000, end_time: 12000, text: "b" },
    { speaker: "1", begin_time: 4000, end_time: 8000, text: "c" },
    { speaker: "2", begin_time: 14000, end_time: 15000, text: "short" },
  ];
  const spans = speakerSegments(sentences);
  expect(spans).toEqual([
    { id: "0", start: 0, end: 4 },
    { id: "0", start: 8, end: 12 },
  ]);
});

test("invalid embeddings are refused", () => {
  expect(() => normalized(Array(256).fill(0))).toThrow("zero norm");
  expect(() => normalized([NaN])).toThrow("finite");
});

test("a known plus unknown person cannot become a single named ASR cluster", () => {
  expect(matchSpeaker("0", [vector(0), vector(2)], settings).status).toBe("inconsistent");
});
