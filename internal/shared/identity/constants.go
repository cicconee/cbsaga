package identity

const (
	AggregateTypeIdentity = "identity"
)

const (
	IdentityStatusVerified = "VERIFIED"
	IdentityStatusRejected = "REJECTED"
)

const (
	EventTypeIdentityRequested = "VerifyIdentityRequested"
	EventTypeIdentityVerified  = "IdentityVerified"
)

const (
	RouteKeyIdentityCmd = "cmd.identity"
	RouteKeyIdentityEvt = "evt.identity"
)
