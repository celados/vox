import { pickFiles } from "@celados/argc/skill";

export function embedSkill(): Record<string, string> {
  const files = pickFiles(import.meta.dir, ["index.md"]);
  const body = files["index.md"];
  if (body === undefined) throw new Error("Embedded skill body is missing index.md");
  return { "SKILL.md": body };
}
