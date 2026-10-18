# Design

The visual world of <https://tools.vukem.dev>. Mode: **Operate** — people come here to finish a
task, so scanability and consistency outrank expression, and the character lives in precise
details rather than in a hero.

## The world: an instrument, not a service

The reference is a well-made measuring tool: a bench instrument, a good oscilloscope's readout, the
labelling on studio hardware. Things are named, measured and legible. Numbers are the content —
sizes, percentages saved, seconds remaining, pixel dimensions — so the interface treats them as
data to be read rather than as decoration.

What that rules out, deliberately:

- **No hero.** There is nothing to sell. The first viewport is the tool index, searchable, because
  the visitor already knows what they want to convert.
- **No grid of identical cards** of icon + heading + sentence. That is the lazy shape for "here are
  our features" and it hides exactly what someone scanning needs: what goes in and what comes out.
  Tools are a **dense list** with their input and output formats inline, like an index.
- **No big-number metric panel.** Nobody opens a converter to look at statistics.
- **No gradient text, no glass, no coloured left borders.** Emphasis is weight, size and a single
  accent.

## Light and dark

Dark is the default, from the use scene rather than from category habit: this is used in the
evening beside an editor. The system preference wins on first visit; a manual override persists and
is honoured before first paint, so there is no flash of the wrong theme.

Both themes are designed, not derived. Light is a warm paper white, not `#fff`; dark is a
blue-cast near-black, not `#000`, so large surfaces do not vibrate.

## Colour

Four roles, and the accent is deliberately not green or red so those stay semantic.

| Token | Role |
| --- | --- |
| `--color-surface-*` | Four steps: page, raised, sunken, border |
| `--color-ink-*` | Three steps: primary, secondary, faint. Secondary is tinted from the surface hue, never gray |
| `--color-signal` | The accent: a cool cyan-blue, the colour of a trace on an instrument. Links, focus, progress, the active tool |
| `--color-ok` / `--color-warn` / `--color-bad` | Finished, needs attention, failed. Reserved for state and never used decoratively |

Every state is carried by text or a shape as well as by colour: a failed row says why, it is not
merely red.

## Type

A system stack, with the typographic control doing the work a webfont would.

**Open question, flagged rather than hidden:** the craft floor says an own-world page should
self-host a display face, and a system sans is a weaker voice. Adding one is an asset and a licence
decision the owner has not made — any face ships in the repository and goes on `/licenses` — so it
is in `questions-to-dev.md` with two OFL candidates. Until then the identity comes from spacing,
colour, the icon set and the readouts, all of which are decided here.

- Body and UI: the platform sans, `-0.011em` tracking, 1.5 line height.
- Display: the same stack at 600 weight, `-0.03em` tracking at display sizes. Headings are
  balanced (`text-wrap: balance`).
- Measurements: the platform mono with `tabular-nums`, so a progress percentage does not jitter as
  it counts and a column of file sizes aligns. This is the single most instrument-like detail
  available without a webfont, and it is applied to every number that changes.
- Body measure capped at 70ch on prose; the tool index is wider because it is a table, not prose.

## Space and rhythm

A 4px base. Tight inside a group, generous between groups, and more space above a heading than
below it so a heading belongs to what follows. The tool index is dense on purpose: scanning twenty
tools is the job.

## Icons

Authored inline SVG on a 24px grid, 1.75 stroke, round caps, one weight throughout. No icon font,
no emoji standing in for an icon, no second stroke weight. There are few of them and each names a
real action.

## Motion

One authored moment: a conversion's progress. It is the only thing on the page that is genuinely
alive, so it gets the motion budget — the bar eases, the percentage counts, the row settles when it
finishes. Everything else transitions at 120ms or not at all. Exponential ease-out from an
already-visible default, and every animation respects `prefers-reduced-motion`.

## Browser surfaces

The parts not drawn by hand still carry the design, and these ship with defaults belonging to no
design system: text selection, the caret, the focus ring, the scrollbar, and underline offsets. All
five are themed from the palette. This is the cheapest signal that a page was built rather than
assembled.

## Copy

The product's own voice: plain, specific, never cheerful about a failure. Controls name their
action (`Convert`, `Cancel`, `Download SVG`), never `Submit`. Errors name the problem and the
recovery, and come from the server's own taxonomy at `/api/v1/conversion-errors` so the interface
and the API cannot word the same failure differently.

The five-minute expiry is stated wherever a result is, in words, before it matters.
