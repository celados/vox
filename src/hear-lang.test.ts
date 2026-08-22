import { expect, test } from "bun:test";
import { cli } from "@celados/argc";

import { schema } from "./schema.ts";

test("repeated --lang accumulates instead of keeping the last value", async () => {
  let lang: unknown;
  const app = cli({ hear: schema.hear }, { name: "vox", version: "test" });
  await app.run(
    {
      handlers: {
        hear: (options) => {
          lang = options.input.lang;
        },
      },
    },
    ["hear", "--file", "x.wav", "--lang", "zh", "--lang", "en"],
  );
  expect(lang).toEqual(["zh", "en"]);
});

test("a single --lang becomes a one-element array", async () => {
  let lang: unknown;
  const app = cli({ hear: schema.hear }, { name: "vox", version: "test" });
  await app.run(
    {
      handlers: {
        hear: (options) => {
          lang = options.input.lang;
        },
      },
    },
    ["hear", "--file", "x.wav", "--lang", "zh"],
  );
  expect(lang).toEqual(["zh"]);
});
