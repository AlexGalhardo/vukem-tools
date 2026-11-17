// The keyboard shortcuts page (TASK-0237), generated from the editor's own keymap so it cannot drift: the rows are
// SHORTCUTS of web/resources/js/editor/commands/keymap.ts (imported as it is: the file only has type imports, which
// Node strips) and their labels are ui.editor.shortcuts.rows of web/resources/lang/en.json, the text of the
// in-editor cheat sheet. A row the keymap gains shows up on the next `npm run reference`; one that no group below
// names lands in "More shortcuts". Called by generate-reference.ts. Node strips the types.
import fs from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { code, esc, page, table } from "./reference-md.ts";

type Shortcut = { id: string; keys: string; aliases?: string; action: string };
type Keymap = { SHORTCUTS: readonly Shortcut[]; displayCombo: (combo: string, mac: boolean) => string; BINDING_IDS: readonly string[] };

export type ShortcutRow = { id: string; label: string; keys: string; aliases?: string; rebindable: boolean };

const GROUPS: { title: string; ids: string[] }[] = [
	{ title: "Play and move the playhead", ids: ["play_pause", "step_frame", "step_second", "go_to_edge", "next_cut", "shuttle"] },
	{ title: "Markers and range", ids: ["marker", "next_marker", "range_in_out", "range_select", "range_clear"] },
	{
		title: "Cut and arrange",
		ids: ["tool", "split", "split_all", "trim_start", "trim_end", "slide", "roll", "slip", "select_side", "select_all", "delete", "ripple_delete", "duplicate", "copy", "cut", "paste", "rename", "toggle_enabled", "link", "extract_audio", "volume_up", "volume_down"],
	},
	{ title: "Add, import and export", ids: ["add_caption", "add_text", "import", "export"] },
	{ title: "History, zoom and panels", ids: ["undo", "redo", "redo_alt", "zoom_in", "zoom_out", "zoom_fit", "zoom_wheel", "preview_zoom", "toggle_snap", "keyframe_panel", "toggle_panel"] },
	{ title: "The editor", ids: ["palette", "find", "save", "cancel", "next_landmark", "grid_keys", "cheat_sheet"] },
];

export async function readKeymap(root: string): Promise<{ keymap: Keymap; labels: Record<string, string> }> {
	const keymap = (await import(pathToFileURL(path.join(root, "web", "resources", "js", "editor", "commands", "keymap.ts")).href)) as Keymap;
	const lang = JSON.parse(fs.readFileSync(path.join(root, "web", "resources", "lang", "en.json"), "utf8")) as {
		ui: { editor: { shortcuts: { rows: Record<string, string> } } };
	};
	return { keymap, labels: lang.ui.editor.shortcuts.rows };
}

export function shortcutRows(keymap: Keymap, labels: Record<string, string>): ShortcutRow[] {
	return keymap.SHORTCUTS.map((s) => {
		const label = labels[s.id];
		if (!label) throw new Error(`shortcuts: row "${s.id}" of keymap.ts has no ui.editor.shortcuts.rows.${s.id} label in en.json`);
		return { id: s.id, label, keys: s.keys, aliases: s.aliases, rebindable: keymap.BINDING_IDS.includes(s.id) };
	});
}

/** Splits "Mod+Shift+Left, Mod+Shift+Right" into one code span per key; text that is not a key stays prose. */
function keyCells(keys: string, mac: boolean, display: Keymap["displayCombo"]): string {
	if (keys.includes(" ") && !keys.includes(", ")) return esc(keys);
	return keys
		.split(", ")
		.map((k) => code(display(k, mac)))
		.join(", ");
}

export function shortcutsPage(keymap: Keymap, labels: Record<string, string>): string {
	const rows = shortcutRows(keymap, labels);
	const byId = new Map(rows.map((r) => [r.id, r]));
	const used = new Set(GROUPS.flatMap((g) => g.ids));
	const groups = [...GROUPS, { title: "More shortcuts", ids: rows.filter((r) => !used.has(r.id)).map((r) => r.id) }];

	const sections = groups
		.map((g) => ({ title: g.title, rows: g.ids.map((id) => byId.get(id)).filter((r): r is ShortcutRow => r !== undefined) }))
		.filter((g) => g.rows.length > 0)
		.map((g) => {
			const body = table(
				["Does", "Windows and Linux", "macOS", "Also"],
				g.rows.map((r) => [
					esc(r.label) + (r.rebindable ? " (can be changed)" : ""),
					keyCells(r.keys, false, keymap.displayCombo),
					keyCells(r.keys, true, keymap.displayCombo),
					r.aliases ? keyCells(r.aliases, false, keymap.displayCombo) : "",
				]),
			);
			return `## ${g.title}\n\n${body}`;
		});

	return page(
		"All keyboard shortcuts",
		"Every shortcut of the editor, generated from its keymap, with the keys for Windows, Linux and macOS.",
		[
			"This list is generated from the keymap of the editor, the same table the cheat sheet shows when you press `?`, so it always matches the version of the editor it was built from. `Mod` is `Ctrl` on Windows and Linux and `Cmd` on macOS; the two columns spell it out. A shortcut never fires while you type in a field, except `Mod+K`.",
			"For how to change a key and how the command palette reaches every command, see [Keyboard shortcuts](/docs/guide/keyboard-shortcuts).",
			...sections,
		].join("\n\n"),
	);
}
