import type { InferHandlers } from "@celados/argc";

import { c, group } from "@celados/argc";
import { file, ref, toStandardJsonSchema } from "@celados/argc/ref";
import * as v from "valibot";

import { dashscopeAuth, lazyAuth } from "./auth.ts";

const s = toStandardJsonSchema;
const hearVendor = v.optional(v.picklist(["fun", "qwen", "mimo"]), "fun");
const dashscopeVendor = v.optional(v.picklist(["fun", "qwen"]), "fun");

const status = c
  .meta({
    description:
      "Preflight: whether each provider key resolves (env, then vault), with sources tried. Never prints key material",
  })
  .input(s(v.object({})));

const hear = c
  .meta({
    description:
      "Transcribe audio into a cached run and Markdown file, optionally identify enrolled speakers",
    examples: ["vox hear \"{ file: 'lecture.mp3', lang: ['zh'], speakers: true }\""],
  })
  .input(
    s(
      v.objectAsync({
        ...lazyAuth,
        file: v.pipe(v.string(), v.minLength(1)),
        model: hearVendor,
        vocab: v.optional(v.string()),
        lang: v.optional(v.array(v.string())),
        speakers: v.optional(v.boolean(), false),
        identify: v.optional(v.boolean(), false),
        speakerIds: v.optional(v.array(v.string())),
        output: v.optional(v.string()),
        refresh: v.optional(v.boolean(), false),
      }),
    ),
  )
  .positional("file");

const speakerId = v.pipe(v.string(), v.regex(/^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$/));
const seconds = v.pipe(v.number(), v.minValue(0));
const ratio = v.pipe(v.number(), v.minValue(0), v.maxValue(1));
const speaker = group(
  { description: "Enroll and identify speakers locally on Apple Silicon with MLX" },
  {
    enroll: c
      .meta({
        description: "Enroll a named speaker from 3–30 seconds of clean solo audio",
      })
      .input(
        s(
          v.object({
            id: speakerId,
            name: v.pipe(v.string(), v.minLength(1)),
            file: v.pipe(v.string(), v.minLength(1)),
            start: v.optional(seconds, 0),
            end: v.optional(seconds),
            replace: v.optional(v.boolean(), false),
          }),
        ),
      ),
    list: c
      .meta({ description: "List enrolled speaker IDs, names and matching settings" })
      .input(s(v.object({}))),
    remove: c
      .meta({ description: "Remove an enrolled speaker by ID" })
      .input(s(v.object({ id: speakerId })))
      .positional("id"),
    config: c
      .meta({ description: "Read or update cosine threshold and minimum candidate margin" })
      .input(s(v.object({ threshold: v.optional(ratio), margin: v.optional(ratio) }))),
    identify: c
      .meta({
        description: "Identify speakers in a stored run without uploading or repeating ASR",
      })
      .input(
        s(
          v.object({
            sid: v.pipe(v.string(), v.minLength(1)),
            speakerIds: v.optional(v.array(speakerId)),
            output: v.optional(v.string()),
          }),
        ),
      )
      .positional("sid"),
  },
);

const session = group(
  { description: "Inspect and delete stored transcription runs" },
  {
    list: c
      .meta({
        description: "List run envelopes, newest first. No transcript — use export for content",
      })
      .input(
        s(
          v.object({
            file: v.optional(v.string()),
            limit: v.optional(v.pipe(v.number(), v.integer(), v.minValue(0)), 20),
          }),
        ),
      ),
    remove: c
      .meta({
        description: "Delete one run by unique sid prefix, or every run with all",
      })
      .input(
        s(
          v.object({
            sid: v.optional(v.string()),
            all: v.optional(v.boolean(), false),
          }),
        ),
      )
      .positional("sid"),
  },
);

const exportCmd = c
  .meta({
    description:
      "Render a stored run as srt, vtt, md, txt, or json. stdout is the document unless output is set",
    examples: ["vox export --sid a3f1c2 --format srt --output meeting.srt"],
  })
  .input(
    s(
      v.object({
        sid: v.pipe(v.string(), v.minLength(1)),
        format: v.picklist(["srt", "vtt", "md", "txt", "json"]),
        output: v.optional(v.string()),
      }),
    ),
  )
  .positional("sid");

const vocab = group(
  { description: "Reconcile local hotword YAML with the server-side lists" },
  {
    list: c
      .meta({ description: "List local vocabularies, sync state, and remote quota" })
      .input(s(v.objectAsync({ ...lazyAuth }))),
    sync: c
      .meta({
        description:
          "Push local YAML to the server for a target model. hear --vocab syncs on demand",
        examples: ["vox vocab.sync --all --model qwen"],
      })
      .input(
        s(
          v.objectAsync({
            ...dashscopeAuth,
            name: v.optional(v.string()),
            all: v.optional(v.boolean(), false),
            model: dashscopeVendor,
            force: v.optional(v.boolean(), false),
          }),
        ),
      )
      .positional("name"),
    prune: c
      .meta({
        description: "Delete server-side lists no local YAML claims",
      })
      .input(s(v.objectAsync({ ...dashscopeAuth, dryRun: v.optional(v.boolean(), false) }))),
  },
);

const say = c
  .meta({
    description:
      "Speak text with TTS. Multi-line text: whole input as YAML with text: |, or a file with text: { ref: 'file:…' }",
    examples: [
      "vox say - <<'EOF'\nvoice: Cherry\ntext: |\n  First line.\n  Slow and clear.\nEOF",
      "vox say \"{ text: { ref: 'file:script.txt' }, voice: 'Cherry' }\"",
    ],
  })
  .input(
    s(
      v.objectAsync({
        ...lazyAuth,
        text: ref(v.pipe(v.string(), v.minLength(1)), [file()], {
          description: "Text to speak, or a text file read verbatim",
        }),
        voice: v.optional(v.string()),
        lang: v.optional(v.string(), "auto"),
        instruct: v.optional(v.string()),
        speed: v.optional(v.number(), 1),
        output: v.optional(v.string()),
        noCache: v.optional(v.boolean(), false),
      }),
    ),
  )
  .positional("text");

const voice = group(
  { description: "List system voices and manage cloned voices" },
  {
    list: c
      .meta({ description: "List system and cloned voices" })
      .input(s(v.objectAsync({ ...lazyAuth }))),
    record: c
      .meta({
        description: "Enroll a cloned voice from a file, or record from the mic on a TTY",
      })
      .input(
        s(
          v.objectAsync({
            ...dashscopeAuth,
            name: v.optional(v.string()),
            file: v.optional(v.string()),
            lang: v.optional(v.string()),
            duration: v.optional(v.pipe(v.number(), v.integer(), v.minValue(1)), 15),
          }),
        ),
      ),
    remove: c
      .meta({ description: "Delete a cloned voice by id" })
      .input(s(v.objectAsync({ ...dashscopeAuth, voiceId: v.pipe(v.string(), v.minLength(1)) })))
      .positional("voiceId"),
  },
);

const cache = group(
  { description: "TTS audio cache (legacy; not content-addressed runs)" },
  {
    status: c
      .meta({ description: "Show cache path, file count, and bytes" })
      .input(s(v.object({}))),
    clear: c.meta({ description: "Delete cached TTS audio" }).input(s(v.object({}))),
  },
);

export const schema = {
  status,
  hear,
  speaker,
  session,
  export: exportCmd,
  vocab,
  say,
  voice,
  cache,
};

export type AppHandlers = InferHandlers<typeof schema>;
