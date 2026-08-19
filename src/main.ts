#!/usr/bin/env bun

import { cli } from "argc";

import packageJson from "../package.json" with { type: "json" };
import { withDomainErrors } from "./cli-error.ts";
import { handlers } from "./handlers.ts";
import { schema } from "./schema.ts";
import { embedSkill } from "./skill.embed.ts" with { type: "macro" };
import { rewriteSayTextArgv } from "./text-body.ts";

const app = cli(schema, {
  name: "vox",
  version: packageJson.version,
  description: "Agent-facing speech to text and text to speech over Alibaba Model Studio.",
  skill: embedSkill(),
});

const argv = rewriteSayTextArgv(process.argv.slice(2));
await app.run({ handlers: withDomainErrors(handlers) }, argv);
