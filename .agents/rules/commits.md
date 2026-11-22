# Commits

The rule is `.claude/rules/commits.md`: Conventional Commits 1.0 in English, `type(scope):
description`, the scope list, imperative lowercase description, header at most 100 characters,
`Refs:` in the body. It is validated by the `commit-msg` hook (`bunx commitlint --edit`) and by CI
on pull requests. Do not copy the rule here; change it there.
