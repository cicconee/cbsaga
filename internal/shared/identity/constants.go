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
	EventTypeIdentityRejected  = "IdentityRejected"
)

const (
	RouteKeyIdentityCmd = "cmd.identity"
	RouteKeyIdentityEvt = "evt.identity"
)
