package orchestrator

const (
	AggregateTypeWithdrawal = "withdrawal"
)

const (
	SagaStateStarted    = "STARTED"
	SagaStateInProgress = "IN_PROGRESS"
	SagaStateFailed     = "FAILED"
)

const (
	SagaStepIdentityCheck = "IDENTITY_CHECK"
	SagaStepRiskCheck     = "RISK_CHECK"
)

const (
	WithdrawalStatusRequested  = "REQUESTED"
	WithdrawalStatusInProgress = "IN_PROGRESS"
	WithdrawalStatusFailed     = "FAILED"
)

const (
	IdemInProgress = "IN_PROGRESS"
	IdemCompleted  = "COMPLETED"
	IdemFailed     = "FAILED"
)

const (
	EventTypeWithdrawalRequested = "WithdrawalRequested"
	EventTypeWithdrawalFailed    = "WithdrawalFailed"
)

const (
	RouteKeyWithdrawalCmd = "cmd.withdrawal"
	RouteKeyWithdrawalEvt = "evt.withdrawal"
)
