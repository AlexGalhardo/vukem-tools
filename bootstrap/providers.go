package bootstrap

import (
	vukem "github.com/AlexGalhardo/vukem"

	"github.com/AlexGalhardo/vukem-tools/app/services"
)

// Providers lists the application providers in registration order;
// vukem make:provider appends here.
func Providers(conversions *services.Conversions) []vukem.Provider {
	return []vukem.Provider{services.Provider(conversions)}
}
