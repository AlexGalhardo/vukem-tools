# Product

Written from the project's own briefs rather than from an interview: the owner was away when the
interface work started. Everything below is drawn from [ROADMAP.md](ROADMAP.md),
[docs/brainstorming.md](docs/brainstorming.md) and [docs/web-research.md](docs/web-research.md),
which are the owner's recorded decisions. **Assumptions are labelled as such** and are the first
thing to correct.

## What it is

An open-source, self-hosted file conversion toolbox at <https://tools.vukem.dev>. Images, video,
audio, documents, archives, and a pixel-art to SVG tracer the owner wrote because nothing
open-source did it well. The whole backend runs on the owner's own Go framework, which is half the
point: the project exists to push that framework as much as to convert files.

## Who uses it

Three readers, in the order they matter:

1. **The owner.** The first and heaviest user, converting files most days. This is the decisive
   audience: a tool that is pleasant for one person who uses it daily beats a tool that demos well.
2. **Game developers and digital-content makers.** The pixel-art tracer, the sprite-sheet work in
   S5 and the video clipping all serve asset production. They arrive with files already in hand and
   a specific conversion in mind.
3. **People who found an open-source alternative to CloudConvert.** They are comparing. What they
   need to establish in seconds is that it is real, that it is free, and what it can actually do.

Everyone has an account: there is no anonymous conversion, so every file has an owner and every
budget has somebody to charge.

## What success looks like

- The owner reaches for this instead of a browser tab full of ad-funded converters.
- A visitor who wants one conversion gets it done without reading anything.
- A visitor comparing against iLovePDF or CloudConvert can find out what is and is not supported
  without guessing — including what is deliberately missing and why.

## The real usage scene

*Assumption, not stated by the owner:* a desktop browser, files already on disk, often late and
often alongside a game engine or an editor. Dark is the default because that is the ambient
condition this is used in, with the system preference honoured and a manual override, which the
ROADMAP does require explicitly.

Mobile matters less but must work: checking a conversion that is already running, and downloading
its result, are the things someone will do from a phone.

## Constraints that shape the interface

- **A result lives 5 minutes.** The interface has to say so next to every download, because a
  user who walks away loses the file. This is the single hardest constraint to design around and
  the easiest to get wrong by hiding it.
- **Login for every tool.** The first run is therefore register → verify email → convert, which is
  three steps before any value. The home page has to be worth that.
- **Conversions are asynchronous and can take minutes.** Progress is real, arrives over a stream,
  and has to survive a page reload.
- **Failures are expected and varied.** Nine error codes, each needing a sentence a person can act
  on. The taxonomy is defined once in Go and published at `/api/v1/conversion-errors`, so the
  interface must read it rather than restate it.
- **English only.** No i18n layer, by decision.
- **Every engine's licence is declared.** Ghostscript is AGPL-3.0 and ffmpeg is built GPL; the
  `/licenses` page is a legal requirement of shipping them, not a courtesy.
- **It must run on a small container.** No design that needs a megabyte of JavaScript.

## What this is not

Not a SaaS landing page: there is nothing to sell, no pricing, no testimonials, no waitlist. Not a
dashboard either — nobody opens this to look at charts. It is a workshop someone walks into with a
file.
