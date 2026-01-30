package orchestratorv1

const (
	EventTypeWithdrawalRequested = "WithdrawalRequested"
	EventTypeWithdrawalFailed    = "WithdrawalFailed"
)

const (
	RouteKeyWithdrawalCmd = "cmd.withdrawal"
	RouteKeyWithdrawalEvt = "evt.withdrawal"
)
