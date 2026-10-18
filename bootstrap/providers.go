package bootstrap

import (
	vukem "github.com/AlexGalhardo/vukem"

	"github.com/AlexGalhardo/vukem-tools/app/services"
)

// Providers lists the application providers in registration order;
// vukem make:provider appends here.
//
// Telemetry is first and that is not cosmetic: registering it installs the
// HTTP middleware and the database and queue observers, so a provider
// registered after the things it instruments would instrument nothing.
func Providers(telemetry *services.Telemetry, conversions *services.Conversions) []vukem.Provider {
	return []vukem.Provider{telemetry, services.Provider(conversions)}
}
