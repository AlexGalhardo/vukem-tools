package routes

import (
	"github.com/AlexGalhardo/vukem"
	"github.com/AlexGalhardo/vukem/router"

	"github.com/AlexGalhardo/vukem-tools/app/services"
)

// All lists the route files, in order; vukem install:* appends here.
//
// Web stays first: it installs the default middleware, the Inertia middleware
// and the shared props that every other group builds on. It takes the
// conversion pipeline because the tool index may only advertise a tool that is
// actually registered.
func All(
	telemetry *services.Telemetry,
	conversions *services.Conversions,
) []func(r *router.Router, app *vukem.App) error {
	return []func(r *router.Router, app *vukem.App) error{
		Web(telemetry, conversions),
		Auth,
		API,
		Social,
		APIDocs,
		Account(conversions),
		Conversions(conversions),
		Media(conversions),
		PDF(conversions),
		Archives(conversions),
		Sprites(conversions),
		Downloads(conversions),
		Uploads(conversions),
	}
}
