package publichttpsrv

import (
	"github.com/reiver/go-pathmux"
)

// Mux is the HTTP router for the public listener — what space hosts call.
//
// Only the notification end-points (com.atproto.space.confirmRegistration, com.atproto.space.notifySpaceUpdate)
// and the space-mirror's did:web document (/.well-known/did.json) are registered here.
var (
	Mux pathmux.Mux
)
