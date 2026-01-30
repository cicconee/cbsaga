package domain

const (
	SagaStateStarted    = "STARTED"
	SagaStateInProgress = "IN_PROGRESS"
	SagaStateFailed     = "FAILED"
)

const (
	SagaStepIdentityCheck = "IDENTITY_CHECK"
	SagaStepRiskCheck     = "RISK_CHECK"
)
