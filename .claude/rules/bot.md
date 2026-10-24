# Telegram bot and device-link rules

- The bot (`cmd/bot`, `internal/bot`) is a client of the HTTP API and holds no key of its own. Each
  person signs in through a device link (`app/devicelink`) and the bot keeps their key in memory
  only. Never write a key, a device code or a Telegram token to disk, to a log or to a chat.
- The server fixes what a bot key may do: `abilities.Telegram()`. The bot cannot ask for
  abilities. When a tool is added to `internal/bot/tools.go`, widen that list in the same change,
  and `TestTheTableAndTheServersGrantAgree` fails until both agree. Never grant `pdf:security`,
  `pdf:edit` or `sprite:sheet` to the bot.
- Offer only what fits a chat: one file or a short list of files and at most one question
  answered with a button. A tool that needs a typed password, a page range or a geometry stays in
  the web interface.
- Everything that crosses the bot's edge is checked with `z` in `internal/bot/validate.go`: updates
  (private chats only), pressed buttons (a value must be one of the tool's own options), files
  (Telegram's 20 MB limit) and the server's answers (result links must be on the API's host). A new
  input or output gets a schema there.
- Every message the bot sends is plain text: file names and titles come from outside. Callback data
  is short, matches `^[A-Za-z0-9:+._-]{1,48}$`, and never carries user text.
- A device link is approved only from a signed-in session of an account with a verified email, and
  one approval yields exactly one key (`TestOnlyOnePollerWinsAnApprovedLink`). Codes expire in ten
  minutes and an unknown code reads as expired.
- Test a conversation against fakes (`internal/bot/fakes_test.go`) and the link flow against the
  real HTTP app (`tests/devicelink_test.go`). Say plainly that the real Telegram Bot API was not
  exercised when it was not.
