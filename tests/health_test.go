package tests

import (
	"testing"

	"github.com/AlexGalhardo/vukem/vukemtest"

	"github.com/AlexGalhardo/vukem-tools/bootstrap"
	"github.com/AlexGalhardo/vukem-tools/internal/httpx"
)

// The deep probe reports every dependency the app cannot work without. A
// shallow 200 on /up says the process is alive; this says it can do its job.
func TestDeepProbeReportsEveryDependency(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)

	r := app.Client(t).GetJSON("/up?checks=1").AssertOK()
	r.AssertJSONPath("status", "ok")
	r.AssertJSONPath("checks.database", "ok")
	r.AssertJSONPath("checks.queue", "ok")
}

// Every response names the deploy that served it, including a 404: with one
// image behind two services and containers replaced on deploy, "which deploy
// answered this?" is otherwise guesswork.
func TestDeployTargetOnEveryResponse(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New, vukemtest.Options{
		Env: map[string]string{"DEPLOY_TARGET": "test-deploy"},
	})

	app.Client(t).Get("/").AssertOK().AssertHeader(httpx.DeployTargetHeader, "test-deploy")
	app.Client(t).Get("/up").AssertOK().AssertHeader(httpx.DeployTargetHeader, "test-deploy")
	app.Client(t).Get("/no-such-page").AssertStatus(404).
		AssertHeader(httpx.DeployTargetHeader, "test-deploy")
}

// An unset DEPLOY_TARGET still stamps a value, so the header's absence always
// means "this middleware is not running" rather than "nothing was configured".
func TestDeployTargetFallsBackToUnknown(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New, vukemtest.Options{
		Env: map[string]string{"DEPLOY_TARGET": ""},
	})

	app.Client(t).Get("/up").AssertOK().AssertHeader(httpx.DeployTargetHeader, "unknown")
}
