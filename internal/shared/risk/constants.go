package risk

const (
	AggregateTypeRisk = "risk"
)

const (
	RiskStatusApproved = "APPROVED"
	RiskStatusRejected = "REJECTED"
)

const (
	EventTypeRiskCheckRequested = "RiskCheckRequested"
	EventTypeRiskCheckApproved  = "RiskCheckApproved"
	EventTypeRiskCheckRejected  = "RiskCheckRejected"
)

const (
	RouteKeyRiskCmd = "cmd.risk"
	RouteKeyRiskEvt = "evt.risk"
)
