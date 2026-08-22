import type { InferHandlers } from "@celados/argc";

import { toStandardJsonSchema } from "@valibot/to-json-schema";
import { c, group } from "@celados/argc";
import * as v from "valibot";

const s = toStandardJsonSchema;
const vendor = v.optional(v.picklist(["fun", "qwen"]), "fun");

const status = c
  .meta({
    description: "Preflight disclosure: authenticated false alone, or service dashscope",
    examples: ["vox status"],
  })
  .input(s(v.object({})));

const auth = group(
  { description: "Authenticate against DashScope (Alibaba Model Studio)" },
  {
    login: c
      .meta({
        description:
          "Store the DashScope API key after validating it. Prompts on a TTY when token is omitted",
        examples: ["vox auth.login", "vox auth.login --token sk-..."],
      })
      .input(s(v.object({ token: v.optional(v.string()) }))),
    logout: c.meta({ description: "Clear stored DashScope credentials" }).input(s(v.object({}))),
    status: c
      .meta({ description: "Auth-only disclosure (same shape as status)" })
      .input(s(v.object({}))),
  },
);

const hear = c
  .meta({
    description:
      "Transcribe an audio file into a content-addressed run. Idempotent unless refresh is set",
    examples: [
      "vox hear --file meeting.m4a",
      "vox hear \"{ file: 'lecture.mp3', lang: ['zh'], speakers: true }\"",
    ],
  })
  .input(
    s(
      v.object({
        file: v.pipe(v.string(), v.minLength(1)),
        model: vendor,
        vocab: v.optional(v.string()),
        lang: v.optional(v.array(v.string())),
        speakers: v.optional(v.boolean(), false),
        refresh: v.optional(v.boolean(), false),
      }),
    ),
  )
  .positional("file");

const session = group(
  { description: "Inspect and delete stored transcription runs" },
  {
    list: c
      .meta({
        description: "List run envelopes, newest first. No transcript — use export for content",
        examples: ["vox session.list", "vox session.list --file meeting.m4a"],
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
        examples: ["vox session.remove --sid a3f1c2", "vox session.remove --all"],
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
    examples: [
      "vox export --sid a3f1c2 --format md",
      "vox export --sid a3f1c2 --format srt --output meeting.srt",
    ],
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
      .input(s(v.object({}))),
    sync: c
      .meta({
        description:
          "Push local YAML to the server for a target model. hear --vocab syncs on demand",
        examples: ["vox vocab.sync --name meeting", "vox vocab.sync --all --model qwen"],
      })
      .input(
        s(
          v.object({
            name: v.optional(v.string()),
            all: v.optional(v.boolean(), false),
            model: vendor,
            force: v.optional(v.boolean(), false),
          }),
        ),
      )
      .positional("name"),
    prune: c
      .meta({
        description: "Delete server-side lists no local YAML claims",
        examples: ["vox vocab.prune --dryRun"],
      })
      .input(s(v.object({ dryRun: v.optional(v.boolean(), false) }))),
  },
);

const say = c
  .meta({
    description: "Speak text with TTS. Prefer --text - for multi-line input",
    examples: [
      "vox say --text 'Hello world' --voice Cherry",
      "vox say --text - <<'TXT'\nSlow and clear.\nTXT",
    ],
  })
  .input(
    s(
      v.object({
        text: v.pipe(v.string(), v.minLength(1)),
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
    list: c.meta({ description: "List system and cloned voices" }).input(s(v.object({}))),
    record: c
      .meta({
        description: "Enroll a cloned voice from a file, or record from the mic on a TTY",
        examples: ["vox voice.record --file sample.wav --name myvoice"],
      })
      .input(
        s(
          v.object({
            name: v.optional(v.string()),
            file: v.optional(v.string()),
            lang: v.optional(v.string()),
            duration: v.optional(v.pipe(v.number(), v.integer(), v.minValue(1)), 15),
          }),
        ),
      ),
    remove: c
      .meta({ description: "Delete a cloned voice by id" })
      .input(s(v.object({ voiceId: v.pipe(v.string(), v.minLength(1)) })))
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
  auth,
  hear,
  session,
  export: exportCmd,
  vocab,
  say,
  voice,
  cache,
};

export type AppHandlers = InferHandlers<typeof schema>;
