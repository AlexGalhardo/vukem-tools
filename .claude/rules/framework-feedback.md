# Framework feedback rule

This project is a battle test for the vukem framework. Every vukem bug, missing feature, awkward
API, security issue or wrong/missing docs page you hit gets one entry appended to
`../framework-feedback/vukem-tools-feedback.md` (format at the top of that file; include the docs
URL for docs findings).

Apply a local workaround, mark it in code with `// workaround(vukem): <entry title>`, and keep
going. Never edit the vukem repository from here.

At the end of every completed task group, also update `../framework-feedback/audit.md` when the
finding changes the framework backlog, and `../status.md` with what shipped and what is next. See
`ROADMAP.md`, section "Framework feedback".
