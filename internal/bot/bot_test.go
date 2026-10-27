package bot_test

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/app/abilities"
	"github.com/AlexGalhardo/vukem-tools/clients/apiclient"
	"github.com/AlexGalhardo/vukem-tools/internal/bot"
)

// ---------------------------------------------------------------------------
// Signing in.
// ---------------------------------------------------------------------------

func TestASignedOutPersonSeesOnlyTheSignInButton(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.say("/start")

	menu := h.tg.last()
	if got := menu.buttons(); len(got) != 1 || got["Sign in"] != "login" {
		t.Fatalf("a signed-out menu has buttons %v, want only Sign in", got)
	}

	// Nothing else is reachable: a forged press of a tool button is answered
	// with the way in, and no API call is made.
	h.press("t:imgconv")

	if h.api.called("image-convert") || !strings.Contains(h.tg.last().text, "Sign in") {
		t.Errorf("a signed-out press of a tool did not lead back to the sign-in: %q", h.tg.last().text)
	}
}

func TestSigningInNeedsNoPasswordInTheChatAndEndsAtTheMainMenu(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.say("/start")
	h.press("login")

	// The sign-in page is a button, and the code to check is in the text.
	prompt := h.tg.everShown("BCDF-2346")
	if prompt == nil {
		t.Fatalf("the sign-in message does not show the code:\n%s", h.tg.texts())
	}

	if !strings.HasPrefix(prompt.buttons()["Open the sign-in page"], "url:"+apiBase+"/account/link") {
		t.Errorf("the page button is %v", prompt.buttons())
	}

	if strings.Contains(h.tg.texts(), "device-code") || strings.Contains(h.tg.texts(), "secret-key") {
		t.Fatal("a secret reached the chat")
	}

	main := h.tg.find("Signed in as Ana Souza (ana@example.com)")
	if main == nil {
		t.Fatalf("the person was not taken to the main menu:\n%s", h.tg.texts())
	}

	if !strings.Contains(main.text, "Today: 2m 00s of 30m 00s used, 28m 00s left, resets at 00:00 UTC") {
		t.Errorf("the menu does not show the quota: %q", main.text)
	}

	buttons := main.buttons()
	for _, want := range []string{"Images", "Audio and video", "PDF", "Documents to PDF", "Archives", "Download a video", "Refresh", "Sign out"} {
		if _, ok := buttons[want]; !ok {
			t.Errorf("the main menu has no %q button: %v", want, buttons)
		}
	}
}

func TestAnUnlimitedAccountSaysSo(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.api.usage = apiclient.UsageView{
		Unlimited:        new(true),
		SpentSeconds:     new(int64(65)),
		BudgetSeconds:    new(int64(0)),
		RemainingSeconds: new(int64(0)),
	}
	h.signIn()

	if m := h.tg.find("Signed in as"); m == nil || !strings.Contains(m.text, "Today: 1m 05s used, no daily limit") {
		t.Errorf("the unlimited line is wrong:\n%s", h.tg.texts())
	}
}

func TestRefusingOrLettingALinkExpireOffersAnotherTry(t *testing.T) {
	t.Parallel()

	for _, status := range []string{"denied", "expired"} {
		h := newHarness(t)
		h.links.statuses = []string{status}
		h.say("/start")
		h.press("login")

		last := h.tg.last()
		if last.buttons()["Sign in again"] != "login" {
			t.Errorf("%s: no way to try again: %q %v", status, last.text, last.buttons())
		}

		if h.tg.find("Signed in as") != nil {
			t.Errorf("%s: the person was signed in anyway", status)
		}
	}
}

func TestTheSignInLinkGoesInTheTextWhenTelegramRefusesTheButton(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.tg.rejectURLButtons = true
	h.say("/start")
	h.press("login")

	if m := h.tg.everShown("BCDF-2346"); m == nil || !strings.Contains(m.text, apiBase+"/account/link?code=BCDF-2346") {
		t.Fatalf("with the button refused, the link must be in the text:\n%s", h.tg.texts())
	}
}

func TestASignInLinkOnAnotherHostIsNotForwarded(t *testing.T) {
	t.Parallel()

	h := newHarness(t, func(o *bot.Options) { o.APIBaseURL = "https://other.example" })
	h.say("/start")
	h.press("login")

	if strings.Contains(h.tg.texts(), "BCDF-2346") {
		t.Fatalf("a link to a host that is not the API's was shown:\n%s", h.tg.texts())
	}
}

func TestSigningOutRevokesTheKeyAndForgetsThePerson(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.signIn()
	h.press("logout")

	if !h.api.revoked {
		t.Error("the key was only forgotten here, not revoked on the server")
	}

	if h.tg.last().buttons()["Sign in"] != "login" {
		t.Errorf("after signing out the menu is %v", h.tg.last().buttons())
	}

	h.press("g:img")

	if h.tg.find("Convert an image") != nil {
		t.Error("a signed-out press still opened a tool group")
	}
}

func TestAKeyThatStopsWorkingSignsThePersonOut(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.signIn()

	h.api.failWith = &apiclient.Error{Status: http.StatusUnauthorized}
	h.press("m")

	if h.tg.last().buttons()["Sign in"] != "login" || !strings.Contains(h.tg.last().text, "signed out") {
		t.Errorf("a 401 did not lead back to the sign-in: %q", h.tg.last().text)
	}
}

// ---------------------------------------------------------------------------
// What is let in.
// ---------------------------------------------------------------------------

func TestAGroupChatIsRefused(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	h.b.Handle(context.Background(), bot.Update{Message: &bot.Message{
		From: &bot.Sender{ID: userID}, Chat: &bot.Chat{ID: -100123, Type: "supergroup"}, Text: "/start",
	}})

	if m := h.tg.last(); !strings.Contains(m.text, "private chat") || h.links.started != 0 {
		t.Errorf("a group was served: %q", m.text)
	}
}

func TestAnUpdateWithNoSenderOrABotSenderIsRefused(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	h.b.Handle(
		context.Background(),
		bot.Update{Message: &bot.Message{Chat: &bot.Chat{ID: chatID, Type: "private"}, Text: "hi"}},
	)

	if h.tg.last().text != "This bot is private." {
		t.Errorf("a message with no sender got %q", h.tg.last().text)
	}

	h.b.Handle(context.Background(), bot.Update{Message: &bot.Message{
		From: &bot.Sender{ID: userID, IsBot: true}, Chat: &bot.Chat{ID: chatID, Type: "private"}, Text: "hi",
	}})

	if h.links.started != 0 {
		t.Error("a bot was allowed to start a sign-in")
	}
}

func TestTheOptionalAllowListKeepsOthersOut(t *testing.T) {
	t.Parallel()

	h := newHarness(t, func(o *bot.Options) { o.AllowedUsers = []int64{1} })
	h.say("/start")

	if h.tg.last().text != "This bot is private." || h.links.started != 0 {
		t.Errorf("a person outside the list got %q", h.tg.last().text)
	}
}

func TestAForgedButtonValueNeverReachesTheAPI(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.signIn()

	for _, data := range []string{"c:../../etc/passwd", "c:" + strings.Repeat("a", 60), "t:../x", "g:%00", "dv:rm -rf", "x:"} {
		h.press(data)
	}

	if len(h.api.calls) != 0 {
		t.Errorf("a forged button reached the API: %v", h.api.calls)
	}

	// A choice the tool never offered is refused even though it is well formed.
	h.press("g:img")
	h.press("t:imgconv")
	h.sendFile("f1", []byte("png-bytes"))
	h.press("c:exe")

	if h.api.called("image-convert") {
		t.Fatal("a value outside the tool's own options was sent")
	}
}

// ---------------------------------------------------------------------------
// Using a tool.
// ---------------------------------------------------------------------------

func TestConvertingAnImageFromPickToResult(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.api.status = []string{"running", "finished"}
	h.api.results = []apiclient.ResultFile{
		{
			Name: new(
				"picture.webp",
			),
			Mime:      new("image/webp"),
			SizeBytes: new(int64(900)),
			URL:       new(apiBase + "/files/abc?sig=1"),
		},
	}
	h.signIn()

	h.press("g:img")

	group := h.tg.last()
	if group.buttons()["Convert an image"] != "t:imgconv" {
		t.Fatalf("the group menu is %v", group.buttons())
	}

	h.press("t:imgconv")

	if !strings.Contains(h.tg.last().text, "Send the image") {
		t.Fatalf("the prompt is %q", h.tg.last().text)
	}

	h.sendFile("f1", []byte("png-bytes"))

	question := h.tg.find("Convert to which format?")
	if question == nil || question.buttons()["WebP"] != "c:webp" {
		t.Fatalf("the question was not asked:\n%s", h.tg.texts())
	}

	if len(h.api.uploaded) != 1 || h.api.uploaded[0] != "png-bytes" {
		t.Errorf("the file did not reach the server intact: %v", h.api.uploaded)
	}

	h.press("c:webp")

	body, ok := h.api.body.(apiclient.ToolsImageConvertBody)
	if !ok || body.FileID != "UP1" || valueOr(body.Options.To) != "webp" {
		t.Fatalf("the conversion was submitted as %+v", h.api.body)
	}

	if len(h.tg.sent) != 1 || !strings.HasPrefix(h.tg.sent[0], "document:"+apiBase+"/files/abc") {
		t.Fatalf("the result was sent as %v", h.tg.sent)
	}

	if !strings.Contains(h.tg.texts(), "Took 3 seconds.") {
		t.Errorf("the conversion's own note was dropped:\n%s", h.tg.texts())
	}

	if h.tg.last().buttons()["Sign out"] != "logout" {
		t.Errorf("the menu did not come back after the result: %v", h.tg.last().buttons())
	}
}

func valueOr[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}

	return *p
}

func TestAFileOfTheWrongKindIsRefusedBeforeAnythingRuns(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.signIn()
	h.press("g:pdf")
	h.press("t:pdfcomp")

	// The fake server detects an image, whatever the name says.
	h.sendFile("f1", []byte("not-a-pdf"), func(m *bot.Message) { m.Document.FileName = "report.pdf" })

	if !strings.Contains(h.tg.last().text, "image/png") || h.api.called("pdf-compress") {
		t.Fatalf("a PNG sent to the PDF tool was accepted: %q", h.tg.last().text)
	}
}

func TestAFileIsNotTakenWithoutAToolOrOverTheLimit(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.signIn()
	h.sendFile("f1", []byte("data"))

	if !strings.Contains(h.tg.texts(), "Pick what to do first") || len(h.api.uploaded) != 0 {
		t.Errorf("a file with no tool picked was uploaded:\n%s", h.tg.texts())
	}

	h.press("g:img")
	h.press("t:imgconv")

	h.sendFile("big", []byte("x"), func(m *bot.Message) { m.Document.FileSize = bot.MaxFileBytes + 1 })

	if len(h.api.uploaded) != 0 || !strings.Contains(h.tg.last().text, "cannot take that file") {
		t.Errorf("a file over the limit was taken: %q", h.tg.last().text)
	}
}

func TestMergingNeedsTwoFilesAndKeepsTheirOrder(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.up.mime = "application/pdf"
	h.signIn()
	h.press("g:pdf")
	h.press("t:pdfmrg")

	h.sendFile(
		"a",
		[]byte("first"),
		func(m *bot.Message) { m.Document.FileName = "a.pdf"; m.Document.MimeType = "application/pdf" },
	)

	if !strings.Contains(h.tg.last().text, "Send the next one") || h.tg.last().buttons()["Done"] != "done" {
		t.Fatalf("after the first file the menu is %q %v", h.tg.last().text, h.tg.last().buttons())
	}

	h.press("done")

	if h.api.called("pdf-merge") || !strings.Contains(h.tg.last().text, "at least 2") {
		t.Fatalf("one file was merged: %q", h.tg.last().text)
	}

	h.sendFile(
		"b",
		[]byte("second"),
		func(m *bot.Message) { m.Document.FileName = "b.pdf"; m.Document.MimeType = "application/pdf" },
	)
	h.press("done")

	body, ok := h.api.body.(apiclient.ToolsPdfMergeBody)
	if !ok || len(body.FileIds) != 2 || body.FileIds[0] != "UP1" || body.FileIds[1] != "UP2" {
		t.Fatalf("the merge was submitted as %+v", h.api.body)
	}
}

func TestAFailedConversionShowsTheServersOwnSentence(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.api.status = []string{"error"}
	h.up.mime = "application/pdf"
	h.signIn()
	h.press("g:pdf")
	h.press("t:pdfcomp")
	h.sendFile("a", []byte("x"), func(m *bot.Message) { m.Document.MimeType = "application/pdf" })
	h.press("c:light")

	if !strings.Contains(h.tg.texts(), "That file is damaged.") {
		t.Errorf("the failure was not reported:\n%s", h.tg.texts())
	}
}

func TestAResultThatIsNotFromTheServerIsNotSent(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.api.results = []apiclient.ResultFile{
		{
			Name:      new("evil.png"),
			Mime:      new("image/png"),
			SizeBytes: new(int64(10)),
			URL:       new("https://evil.example/x.png"),
		},
		{Name: new("bad.png"), Mime: new("not a type"), SizeBytes: new(int64(10)), URL: new(apiBase + "/files/ok")},
		{Name: new("good.png"), Mime: new("image/png"), SizeBytes: new(int64(10)), URL: new(apiBase + "/files/good")},
	}
	h.signIn()
	h.press("g:img")
	h.press("t:imgconv")
	h.sendFile("f1", []byte("x"))
	h.press("c:png")

	if len(h.tg.sent) != 1 || !strings.Contains(h.tg.sent[0], "/files/good") {
		t.Fatalf("results were sent as %v, want only the one from the server that passes validation", h.tg.sent)
	}
}

func TestAConversionTheServerDescribesStrangelyIsNotFollowed(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.api.status = []string{"exploded"}
	h.signIn()
	h.press("g:img")
	h.press("t:imgconv")
	h.sendFile("f1", []byte("x"))
	h.press("c:png")

	if len(h.tg.sent) != 0 || !strings.Contains(h.tg.texts(), "did not expect") {
		t.Errorf("an unknown status was acted on:\n%s", h.tg.texts())
	}
}

func TestAPastedLinkStartsADownloadAndRunsOnlyAKnownFormat(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.api.results = []apiclient.ResultFile{
		{Name: new("v.mp4"), Mime: new("video/mp4"), SizeBytes: new(int64(10)), URL: new(apiBase + "/files/v")},
	}
	h.signIn()
	h.say("https://x.com/someone/status/123")

	offer := h.tg.last()
	if !strings.Contains(offer.text, "A fine video") || offer.buttons()["MP3"] != "dv:audio" ||
		offer.buttons()["MP4"] != "dv:video" {
		t.Fatalf("the offer is %q %v", offer.text, offer.buttons())
	}

	h.press("dv:audio")

	body, ok := h.api.body.(apiclient.ToolsVideoDownloadBody)
	if !ok || body.Options.URL != "https://x.com/someone/status/123" || valueOr(body.Options.Kind) != "audio" {
		t.Fatalf("the download was submitted as %+v", h.api.body)
	}

	if len(h.tg.sent) != 1 || !strings.HasPrefix(h.tg.sent[0], "video:") {
		t.Errorf("the result was sent as %v", h.tg.sent)
	}
}

func TestMP4AsksFullOrSixtySecondCutsBeforeDownloading(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		press string
		clips bool
	}{{"dv:full", false}, {"dv:clips", true}} {
		h := newHarness(t)
		h.api.results = []apiclient.ResultFile{
			{Name: new("v.mp4"), Mime: new("video/mp4"), SizeBytes: new(int64(10)), URL: new(apiBase + "/files/v")},
		}
		h.signIn()
		h.say("https://www.tiktok.com/@a/video/1")
		h.press("dv:video")

		if h.api.body != nil {
			t.Fatalf("%s: the download started before the second question: %+v", tc.press, h.api.body)
		}

		if got := h.tg.last().buttons(); got["Full video"] != "dv:full" || got["60-second cuts"] != "dv:clips" {
			t.Fatalf("%s: the second question is %v", tc.press, got)
		}

		h.press(tc.press)

		body, ok := h.api.body.(apiclient.ToolsVideoDownloadBody)
		if !ok || valueOr(body.Options.Kind) != "video" || valueOr(body.Options.Clips) != tc.clips {
			t.Errorf("%s: the download was submitted as %+v", tc.press, h.api.body)
		}
	}
}

func TestAnAnswerToTheSecondQuestionBeforeTheFirstIsRefused(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.signIn()
	h.say("https://www.instagram.com/reel/abc/")
	h.press("dv:clips")

	if h.api.body != nil {
		t.Errorf("a download started without a format: %+v", h.api.body)
	}
}

func TestOnlyLinksAreTakenAsLinks(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.signIn()

	for _, text := range []string{"ftp://example.com/x", "javascript:alert(1)", "https://user:pw@example.com/x", "https://exa mple.com/x", "just some words"} {
		h.say(text)
	}

	if len(h.api.calls) != 0 {
		t.Errorf("a non-link reached the download: %v", h.api.calls)
	}
}

// ---------------------------------------------------------------------------
// The tool table.
// ---------------------------------------------------------------------------

func TestTheTableAndTheServersGrantAgree(t *testing.T) {
	t.Parallel()

	granted := abilities.Telegram()

	used := bot.UsedAbilities()
	if strings.Join(used, ",") != strings.Join(sorted(granted), ",") {
		t.Errorf("the bot's menus use %v but the server grants %v: widen or narrow one of them", used, sorted(granted))
	}

	for _, a := range granted {
		if !abilities.Valid(a) {
			t.Errorf("the grant has %q, which is not an ability", a)
		}
	}
}

func sorted(in []string) []string {
	out := append([]string(nil), in...)

	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}

	return out
}

func TestEveryButtonFitsTelegramsLimitAndTheValidators(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.signIn()

	data := regexp.MustCompile(`^[A-Za-z0-9:+._-]{1,48}$`)
	seen := map[string]bool{}

	walk := func(press string) {
		if seen[press] {
			return
		}

		seen[press] = true

		h.press(press)

		for _, d := range h.tg.last().buttons() {
			if strings.HasPrefix(d, "url:") {
				continue
			}

			if !data.MatchString(d) || len(d) > 64 {
				t.Errorf("button data %q is not acceptable", d)
			}
		}
	}

	for _, g := range []string{"img", "av", "pdf", "doc", "arc"} {
		walk("g:" + g)
	}

	for _, id := range []string{"imgconv", "audconv", "vidconv", "pdfcomp", "pdftxt", "pdfocr", "arcz"} {
		walk("t:" + id)
	}
}

func TestAnErrorFromTheServerIsShownInItsOwnWords(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.signIn()
	h.api.failWith = &apiclient.Error{
		Status:  http.StatusTooManyRequests,
		Problem: apiclient.Problem{Detail: "You have used today's conversion budget."},
	}
	h.press("g:img")
	h.press("t:imgconv")
	h.sendFile("f1", []byte("x"))
	h.press("c:png")

	if !strings.Contains(h.tg.texts(), "You have used today's conversion budget.") {
		t.Errorf("the quota refusal was not shown:\n%s", h.tg.texts())
	}
}

func TestAnUploadTheServerRefusesIsExplained(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.signIn()
	h.up.fail = &bot.UploadError{Status: http.StatusUnsupportedMediaType}
	h.press("g:img")
	h.press("t:imgconv")
	h.sendFile("f1", []byte("x"))

	if !strings.Contains(h.tg.last().text, "cannot read that kind of file") {
		t.Errorf("the refusal was %q", h.tg.last().text)
	}

	if errors.Is(h.up.fail, context.Canceled) {
		t.Fatal("unreachable")
	}
}
