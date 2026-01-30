package app

const (
	SubjectWithdrawalCreate = "create_withdrawal"

	StepReserveIdempotencyBeginTx  = "reserve_idempotency_begin_tx"
	StepReserveIdempotencyCommitTx = "reserve_idempotency_commit_tx"
	StepReserveIdempotencyTx       = "reserve_idempotency_tx"

	StepCreateWithdrawalBeginTx  = "create_withdrawal_begin_tx"
	StepCreateWithdrawalCommitTx = "create_withdrawal_commit_tx"
	StepCreateWithdrawalTx       = "create_withdrawal_tx"

	StepFinalizeIdempotencyCompleted = "finalize_idempotency_completed"
	StepFinalizeIdempotencyFailed    = "finalize_idempotency_failed"

	StepEncodeIdentityPayload   = "encode_identity_payload"
	StepEncodeWithdrawalPayload = "encode_withdrawal_payload"

	StepReconcileGetIdempotency        = "reconcile_get_idempotency"
	StepReconcileGetWithdrawal         = "reconcile_get_withdrawal"
	StepReconcileIdempotencyFailed     = "reconcile_idempotency_failed"
	StepReconcileWithdrawalInProgress  = "reconcile_withdrawal_in_progress"
	StepReconcileIdempotencyInProgress = "reconcile_idempotency_in_progress"
	StepReconcileUnknownIdemStatus     = "reconcile_unknown_idem_status"
)
