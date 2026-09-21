import { expect, test } from "bun:test";

import { completionText, MIMO_MAX_BASE64_BYTES, mimoAudioData, mimoLanguage } from "./mimo.ts";

test("MiMo language accepts auto or one supported language", () => {
  expect(mimoLanguage(undefined)).toBe("auto");
  expect(mimoLanguage([])).toBe("auto");
  expect(mimoLanguage(["zh"])).toBe("zh");
  expect(mimoLanguage(["en"])).toBe("en");
  expect(() => mimoLanguage(["zh", "en"])).toThrow("one language");
  expect(() => mimoLanguage(["yue"])).toThrow("one language");
});

test("MiMo audio creates the documented data URL and enforces its encoded limit", () => {
  expect(mimoAudioData(new TextEncoder().encode("audio"), "mp3")).toBe(
    "data:audio/mpeg;base64,YXVkaW8=",
  );
  expect(() => mimoAudioData(new Uint8Array(1), "m4a")).toThrow("only MP3 or WAV");

  const oversized = new Uint8Array(Math.ceil((MIMO_MAX_BASE64_BYTES * 3) / 4) + 1);
  expect(() => mimoAudioData(oversized, "wav")).toThrow("10 MB Base64 limit");
});

test("MiMo response parser reads only assistant text", () => {
  expect(
    completionText({ choices: [{ message: { role: "assistant", content: " transcript " } }] }),
  ).toBe("transcript");
  expect(completionText({ choices: [] })).toBe("");
});
