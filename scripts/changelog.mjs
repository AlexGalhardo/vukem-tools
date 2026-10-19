// Generates CHANGELOG.md with git-cliff and normalises its blank lines.
//
// git-cliff's `trim` is all-or-nothing. On, it strips the blank lines
// markdownlint requires between the header, each version heading and the
// link definitions (MD022, MD034). Off, it leaves runs of blank lines
// (MD012). Neither setting produces a file that passes `lint:md`, and
// CHANGELOG.md must never be hand-edited, so the collapse belongs here --
// part of generating the file, not a step somebody has to remember.
//
//   bun run changelog -- --tag v0.1.0
import { spawnSync } from "node:child_process";
import { readFileSync, writeFileSync } from "node:fs";

const FILE = "CHANGELOG.md";
const args = process.argv.slice(2);

const cliff = spawnSync("git", ["cliff", ...args, "-o", FILE], { stdio: "inherit" });
if (cliff.status !== 0) process.exit(cliff.status ?? 1);

const before = readFileSync(FILE, "utf8");
const after = before.replace(/\n{3,}/g, "\n\n").replace(/\s+$/, "\n");

if (after !== before) {
  writeFileSync(FILE, after);
  console.log(`${FILE}: collapsed blank lines git-cliff could not`);
}
