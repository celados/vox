import { expect, test } from "bun:test";
import { mkdtemp } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import packageJson from "../package.json" with { type: "json" };

const ENTRY = join(import.meta.dir, "main.ts");

async function run(args: string[], home?: string) {
  const proc = Bun.spawn(["bun", "run", ENTRY, ...args], {
    stdout: "pipe",
    stderr: "pipe",
    env: { ...process.env, HOME: home ?? process.env.HOME },
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
    "status()",
    "auth:",
    "login(",
    "hear(input: {",
    "session:",
    "list(",
    "remove(",
    "export(input: {",
    "vocab:",
    "sync(",
    "prune(",
    "say(input: {",
    "voice:",
    "record(",
    "cache:",
  ]) {
    expect(stdout).toContain(fragment);
  }
  expect(stdout).not.toContain("dashscope(");
});

test("@skill prints the embedded guide", async () => {
  const { stdout, stderr, exitCode } = await run(["@skill"]);
  expect(exitCode, stderr).toBe(0);
  expect(stdout).toContain("vox @schema");
  expect(stdout).toContain("There is no realtime ASR");
});

test("status discloses unauthenticated without token material", async () => {
  const home = await mkdtemp(join(tmpdir(), "vox-home-"));
  const { stdout, stderr, exitCode } = await run(["status"], home);
  expect(exitCode, stderr).toBe(0);
  expect(stdout).toContain("authenticated: false");
  expect(stdout).not.toContain("api_key");
  expect(stdout).not.toContain("sk-");
});

test("hear without credentials is not_authenticated", async () => {
  const home = await mkdtemp(join(tmpdir(), "vox-home-"));
  const { stderr, exitCode } = await run(["hear", "--file", "missing.wav"], home);
  expect(exitCode).toBe(1);
  expect(stderr).toContain("error: DOMAIN_ERROR");
  expect(stderr).toContain("code: not_authenticated");
});

test("session.remove rejects path traversal before touching the filesystem", async () => {
  const home = await mkdtemp(join(tmpdir(), "vox-home-"));
  const { stderr, exitCode } = await run(["session.remove", "--sid", ".."], home);
  expect(exitCode).toBe(1);
  expect(stderr).toContain("code: session_not_found");
  expect(stderr).not.toContain("RUNTIME_ERROR");
});
