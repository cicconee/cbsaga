package identityv1

const (
	EventTypeIdentityRequested = "VerifyIdentityRequested"
	EventTypeIdentityVerified  = "IdentityVerified"
	EventTypeIdentityRejected  = "IdentityRejected"
)

const (
	RouteKeyIdentityCmd = "cmd.identity"
	RouteKeyIdentityEvt = "evt.identity"
)
