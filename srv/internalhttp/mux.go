package internalhttpsrv

import (
	"github.com/reiver/go-pathmux"
)

// Mux is the HTTP router for the internal listener — what our own services call.
//
// The read, change-log, status, and admin end-points are registered here.
var (
	Mux pathmux.Mux
)
