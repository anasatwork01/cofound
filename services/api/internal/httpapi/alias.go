package httpapi

import "github.com/anasatwork01/cofound/packages/chassis/config"

// chassisLoader aliases the shared loader, so Bind's signature matches
// chassis.Service.Bind without a second import name at the call site.
type chassisLoader = config.Loader
