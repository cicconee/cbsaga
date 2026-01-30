package riskv1

const (
	EventTypeRiskCheckRequested = "RiskCheckRequested"
	EventTypeRiskCheckApproved  = "RiskCheckApproved"
	EventTypeRiskCheckRejected  = "RiskCheckRejected"
)

const (
	RouteKeyRiskCmd = "cmd.risk"
	RouteKeyRiskEvt = "evt.risk"
)
