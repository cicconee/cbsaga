package app

const (
	SubjectWithdrawalCreate = "create_withdrawal"

	StepReserveIdempotency  = "reserve_idempotency"
	StepCreateWithdrawal    = "create_withdrawal"
	StepFinalizeIdempotency = "finalize_idempotency"
	StepReconcile           = "reconcile"
)
