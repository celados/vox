import { expect, test } from "bun:test";

import { ceilDiv, wavDuration, wrapPcmAsWav } from "./audio.ts";

test("ceilDiv rounds up", () => {
  expect(ceilDiv(10, 3)).toBe(4);
  expect(ceilDiv(0, 3)).toBe(0);
  expect(ceilDiv(3, 0)).toBe(0);
});

test("wrapPcmAsWav then wavDuration round-trips seconds", () => {
  const pcm = new Uint8Array(24_000 * 2 * 2); // 2 seconds of 16-bit mono 24kHz
  const wav = wrapPcmAsWav(pcm, 24_000);
  expect(wavDuration(wav)).toBe(2);
  expect(String.fromCharCode(...wav.subarray(0, 4))).toBe("RIFF");
  expect(String.fromCharCode(...wav.subarray(8, 12))).toBe("WAVE");
});
