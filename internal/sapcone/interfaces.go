package sapcone

import (
	"context"

	"github.com/stellar/go-stellar-sdk/txnbuild"
)

// ParticipantWalletStore abstracts all persistence for participant wallets.
// Implementations may use a real Postgres connection (for integration tests
// and production) or an in-memory mock (for unit tests).
type ParticipantWalletStore interface {
	// GetByPhoneNumber returns the wallet record for phoneNumber, or
	// ErrParticipantNotFound if no record exists.
	GetByPhoneNumber(ctx context.Context, phoneNumber string) (*ParticipantWallet, error)

	// Create inserts a new wallet record in Pending status.
	// Returns ErrParticipantAlreadyExists if phoneNumber is already present.
	Create(ctx context.Context, phoneNumber string) (*ParticipantWallet, error)

	// UpdateStatus persists a status transition and, optionally, a failure
	// reason. Callers should validate the transition via the state machine
	// before calling this.
	UpdateStatus(ctx context.Context, id string, status ParticipantWalletStatus, reason ProvisioningFailureReason) error

	// UpdateStellarAddress records the generated public key after the
	// on-chain account is successfully created.
	UpdateStellarAddress(ctx context.Context, id string, stellarAddress string) error
}

// StellarClient abstracts the subset of Horizon/network operations needed for
// provisioning. The narrow surface makes it easy to mock in unit tests without
// pulling in the full horizonclient.ClientInterface.
type StellarClient interface {
	// LoadAccount fetches the current sequence number and balances for the
	// given address. Used to check treasury balance and to build transactions.
	LoadAccount(ctx context.Context, address string) (StellarAccount, error)

	// SubmitTransaction submits a built transaction to the network, signed
	// by the provided signers.
	SubmitTransaction(ctx context.Context, tx *txnbuild.Transaction, signers ...string) (StellarTransactionResult, error)
}

// StellarProvisioner is the main domain service that drives the two-step
// (CreateAccount + ChangeTrust) lifecycle for a single participant.
type StellarProvisioner interface {
	// Provision ensures the participant identified by phoneNumber has an
	// on-chain account with a trustline to asset.
	//
	// Behaviour by current state:
	//   - Not found:  creates wallet record, creates account, establishes trustline → Ready
	//   - Pending:    resumes provisioning (safe to retry)
	//   - Ready:      no-op; returns ProvisioningResult{AlreadyReady: true}
	//   - Failed:     returns the recorded failure; caller decides whether to retry
	Provision(ctx context.Context, phoneNumber string, asset ProvisioningAsset, treasury TreasuryAccountInfo) (ProvisioningResult, error)
}

// WalletResolver resolves a phone number to its existing wallet or recognises
// it as new. It deliberately performs NO on-chain work — that is the
// StellarProvisioner's job.
type WalletResolver interface {
	// Resolve returns ResolutionOutcomeExisting with the wallet record when
	// phoneNumber is already known, or ResolutionOutcomeNeedsNew with a nil
	// wallet when it is not. It does NOT create a record.
	Resolve(ctx context.Context, phoneNumber string) (ResolutionResult, error)
}

// BatchEligibilityFilter determines which participants in a batch may proceed
// to disbursement based on their wallet provisioning status.
type BatchEligibilityFilter interface {
	// FilterEligible partitions batch into eligible and excluded sets.
	// Individual participant failures are reported in Excluded, not as errors.
	// A returned error means the entire filter operation failed (e.g. DB down).
	FilterEligible(ctx context.Context, batch []BatchParticipant) (BatchEligibilityResult, error)
}

// OnPaymentSettledHook is reserved for the M-Pesa/Daraja payout-leg task.
// The Provisioner MUST NOT depend on it. Out of scope for this session.
type OnPaymentSettledHook interface {
	OnSettled(ctx context.Context, phoneNumber, stellarAddress, amount string) error
}
