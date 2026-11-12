import { pickFiles } from "@celados/argc/skill";

export function embedSkill(): Record<string, string> {
  const { "index.md": body, ...references } = pickFiles(import.meta.dir, [
    "index.md",
    "references/**/*.md",
  ]);
  if (body === undefined) throw new Error("Embedded skill body is missing index.md");
  return { "SKILL.md": body, ...references };
}
