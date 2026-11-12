import { expect, test } from "bun:test";
import { mkdtemp } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import packageJson from "../package.json" with { type: "json" };

const ENTRY = join(import.meta.dir, "main.ts");

async function run(
  args: string[],
  home?: string,
  env: Record<string, string> = {},
  stdin?: string,
) {
  // Provider keys come only from what a test passes; the latch resolver is off under bun test.
  const { DASHSCOPE_API_KEY: _d, MIMO_API_KEY: _m, ...inherited } = process.env;
  const proc = Bun.spawn(["bun", "run", ENTRY, ...args], {
    stdin: stdin === undefined ? "ignore" : new TextEncoder().encode(stdin),
    stdout: "pipe",
    stderr: "pipe",
    env: { ...inherited, NODE_ENV: "test", HOME: home ?? process.env.HOME, ...env },
  });
  const [stdout, stderr, exitCode] = await Promise.all([
    new Response(proc.stdout).text(),
    new Response(proc.stderr).text(),
    proc.exited,
  ]);
  return { stdout, stderr, exitCode };
}

test("--version matches package.json", async () => {
  const { stdout, stderr, exitCode } = await run(["--version"]);
  expect(exitCode, stderr).toBe(0);
  expect(stdout.trim()).toBe(packageJson.version);
});

test("@schema exposes the dotted command surface", async () => {
  const { stdout, stderr, exitCode } = await run(["@schema"]);
  expect(exitCode, stderr).toBe(0);
  for (const fragment of [
    "### status\n",
    "### hear\n",
    "### session.*\n",
    "### session.list\n",
    "### session.remove\n",
    "### export\n",
    "### vocab.*\n",
    "### vocab.sync\n",
    "### vocab.prune\n",
    "### say\n",
    "### voice.*\n",
    "### voice.record\n",
    "### cache.*\n",
  ]) {
    expect(stdout).toContain(fragment);
  }
  expect(stdout).not.toContain("### dashscope");
  expect(stdout).not.toContain("### auth");
  expect(stdout).not.toContain("values:");
  // Key fields are named once (addGlobalDefs), not repeated per command.
  expect(stdout).toContain(
    "type DashScopeKey = string | { ref: `env:${string}` | `bw://${string}/${string}` }",
  );
  expect(stdout).toContain("{ apiKey?: DashScopeKey; dryRun?: boolean = false }");
  expect(stdout).toContain(
    "### vocab.list\nList local vocabularies, sync state, and remote quota\n{ apiKey?: ProviderKey }",
  );
  expect(stdout).toContain("text: string | { ref: `file:${string}` }");
  expect(stdout).toContain("env:DASHSCOPE_API_KEY → bw://");
  expect(stdout).toContain("env:MIMO_API_KEY → bw://");
});

test("@skill prints the embedded guide", async () => {
  const { stdout, stderr, exitCode } = await run(["@skill"]);
  expect(exitCode, stderr).toBe(0);
  expect(stdout).toContain("vox @schema");
  expect(stdout).toContain("No realtime ASR");
});

test("status reports each provider key without key material", async () => {
  const home = await mkdtemp(join(tmpdir(), "vox-home-"));
  const { stdout, stderr, exitCode } = await run(["status"], home, {
    MIMO_API_KEY: "mimo-test-key",
  });
  expect(exitCode, stderr).toBe(0);
  expect(stdout).toMatch(/mimo:\s*\n\s*available: true/);
  expect(stdout).toMatch(/dashscope:\s*\n\s*available: false/);
  expect(stdout).toContain("disabled_in_test");
  expect(stdout).not.toContain("mimo-test-key");
});

test("schema exposes MiMo as an ASR provider", async () => {
  const { stdout, stderr, exitCode } = await run(["@schema"]);
  expect(exitCode, stderr).toBe(0);
  expect(stdout).toContain('model?: "fun" | "qwen" | "mimo"');
});

test("a command needing a missing key fails validation naming each reference", async () => {
  const home = await mkdtemp(join(tmpdir(), "vox-home-"));
  const { stderr, exitCode } = await run(["vocab.prune", "--dryRun"], home);
  expect(exitCode).toBe(1);
  expect(stderr).toContain("INVALID_INPUT");
  expect(stderr).toContain("env:DASHSCOPE_API_KEY (not set)");
  expect(stderr).toContain("disabled_in_test");
});

test("an explicit env ref supplies the key; a secret is never echoed", async () => {
  const home = await mkdtemp(join(tmpdir(), "vox-home-"));
  // Validation resolves the ref; the handler then stops before any network call.
  const ok = await run(["vocab.sync", "{ apiKey: { ref: 'env:VOX_TEST_KEY' } }"], home, {
    VOX_TEST_KEY: "sk-ref-test-key",
  });
  expect(ok.exitCode).toBe(1);
  expect(ok.stderr).toContain("code: invalid_usage");
  expect(ok.stderr).toContain("no vocabulary name given");
  expect(ok.stdout + ok.stderr).not.toContain("sk-ref-test-key");

  const argv = await run(["vocab.prune", "{ apiKey: 'sk-argv-literal' }"], home);
  expect(argv.exitCode).toBe(1);
  expect(argv.stderr).toContain("a literal secret on the command line");
  expect(argv.stdout + argv.stderr).not.toContain("sk-argv-literal");

  const bad = await run(["vocab.prune", "-"], home, {}, "apiKey: '   '\n");
  expect(bad.exitCode).toBe(1);
  expect(bad.stderr).toContain("value hidden");

  const foreign = await run(["vocab.prune", "{ apiKey: { ref: 'file:/etc/passwd' } }"], home);
  expect(foreign.exitCode).toBe(1);
  expect(foreign.stderr).toContain("not a reference this field accepts");
});

test("say treats --text - as literal text, not a stdin read", async () => {
  const home = await mkdtemp(join(tmpdir(), "vox-home-"));
  const { stderr, exitCode } = await run(["say", "--text", "-"], home);
  // `-` is a literal value now; with no key the provider call fails, not a stdin read.
  expect(exitCode).toBe(1);
  expect(stderr).toContain("key_unavailable");
});

test("say reads text from a file reference", async () => {
  const home = await mkdtemp(join(tmpdir(), "vox-home-"));
  const script = join(home, "script.txt");
  await Bun.write(script, "第一行。\nSecond line.\n");
  // The file resolves during validation; with no key the provider call fails next.
  const ok = await run(["say", `{ text: { ref: 'file:${script}' } }`], home);
  expect(ok.exitCode).toBe(1);
  expect(ok.stderr).toContain("key_unavailable");

  const missing = await run(["say", `{ text: { ref: 'file:${join(home, "nope.txt")}' } }`], home);
  expect(missing.exitCode).toBe(1);
  expect(missing.stderr).toContain("INVALID_INPUT");
  expect(missing.stderr).not.toContain("key_unavailable");
});

test("session.remove rejects path traversal before touching the filesystem", async () => {
  const home = await mkdtemp(join(tmpdir(), "vox-home-"));
  const { stderr, exitCode } = await run(["session.remove", "--sid", ".."], home);
  expect(exitCode).toBe(1);
  expect(stderr).toContain("code: session_not_found");
  expect(stderr).not.toContain("RUNTIME_ERROR");
});

test("speaker command schemas preserve enrollment, matching settings, and identity fields", async () => {
  const home = await mkdtemp(join(tmpdir(), "vox-speaker-cli-"));
  const schema = await run(["@schema", ".speaker"], home);
  expect(schema.exitCode, schema.stderr).toBe(0);
  expect(schema.stdout).toContain("### speaker.enroll\n");
  expect(schema.stdout).toMatch(/\n  id: string\n  name: string\n  file: string\n/);
  expect(schema.stdout).toContain("threshold?: number");
  const config = await run(["speaker.config", "{ threshold: 0.72, margin: 0.12 }"], home);
  expect(config.exitCode, config.stderr).toBe(0);
  expect(config.stdout).toContain("threshold: 0.72");
  const list = await run(["speaker.list"], home);
  expect(list.stdout).toContain("margin: 0.12");
  const invalid = await run(
    ["speaker.enroll", "{ id: 'alice', name: 'Alice', file: 'missing.wav', start: 0, end: 1 }"],
    home,
  );
  expect(invalid.stderr).toContain("code: invalid_usage");
});
