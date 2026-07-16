package sapcone

import "time"

// ParticipantWalletStatus tracks the Stellar-provisioning lifecycle of a single
// participant's wallet. It is intentionally separate from SDP's
// ReceiversWalletStatus because the provisioning lifecycle predates and is
// independent of the SEP-24 registration flow.
type ParticipantWalletStatus string

const (
	// ParticipantWalletStatusPending is the initial state when a new phone
	// number is first encountered. Provisioning has not yet started or is
	// in-flight.
	ParticipantWalletStatusPending ParticipantWalletStatus = "PENDING"

	// ParticipantWalletStatusReady means the Stellar account exists on-chain
	// and the required trustline has been established. This participant is
	// eligible for inclusion in disbursement batches.
	ParticipantWalletStatusReady ParticipantWalletStatus = "READY"

	// ParticipantWalletStatusFailed means at least one provisioning step
	// failed with a recorded reason. Manual intervention or a retry policy
	// is required before the participant can be moved to Ready.
	ParticipantWalletStatusFailed ParticipantWalletStatus = "FAILED"
)

// ProvisioningFailureReason gives callers a typed reason for a Failed status so
// they can distinguish account-creation failures from trustline failures and
// take appropriate action.
type ProvisioningFailureReason string

const (
	ProvisioningFailureReasonNone                     ProvisioningFailureReason = ""
	ProvisioningFailureReasonAccountCreationFailed    ProvisioningFailureReason = "ACCOUNT_CREATION_FAILED"
	ProvisioningFailureReasonInsufficientTreasury     ProvisioningFailureReason = "INSUFFICIENT_TREASURY_BALANCE"
	ProvisioningFailureReasonTrustlineEstablishFailed ProvisioningFailureReason = "TRUSTLINE_ESTABLISHMENT_FAILED"
)

// ParticipantWallet is the read model for a participant's Stellar wallet.
// It is the source of truth for whether a phone number already has a wallet.
type ParticipantWallet struct {
	ID             string
	PhoneNumber    string
	StellarAddress string
	StellarSeed    string // encrypted at rest; never logged
	Status         ParticipantWalletStatus
	FailureReason  ProvisioningFailureReason
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// TreasuryAccountInfo is passed to the StellarProvisioner so it can check
// balances and sign as the treasury.
type TreasuryAccountInfo struct {
	// PublicKey is the Stellar G… address of the treasury account.
	PublicKey string
	// Seed is the raw secret key used to sign transactions. Never logged.
	Seed string
}

// StellarAccount is the minimal read model returned by StellarClient.LoadAccount.
// Only the fields our provisioning logic actually needs are included.
type StellarAccount struct {
	Address        string
	SequenceNumber int64
	// NativeBalance is the XLM balance as a string (matches Horizon's wire format).
	NativeBalance string
}

// StellarTransactionResult contains the subset of a Horizon submission result
// that provisioning logic cares about.
type StellarTransactionResult struct {
	Hash       string
	Successful bool
}

// ProvisioningAsset identifies the Stellar asset for which a trustline must be
// established. Separate from data.Asset to avoid a circular import between
// sapcone and the data package in unit-test scenarios.
type ProvisioningAsset struct {
	Code   string
	Issuer string
}

// ProvisionerOptions captures the on-chain parameters the ProvisionerService
// is built with.
type ProvisionerOptions struct {
	NetworkPassphrase string
	StartingBalance   string
	// OnPaymentSettled is a no-op hook reserved for the M-Pesa/Daraja payout
	// leg. Out of scope to implement; stub only.
	OnPaymentSettled OnPaymentSettledHook
}

// ProvisioningResult is returned by Provision to give callers full context on
// what happened, whether success or failure.
type ProvisioningResult struct {
	WalletID               string
	StellarAddress         string
	AlreadyReady           bool
	AccountCreatedTxHash   string
	TrustlineCreatedTxHash string
	FailureReason          ProvisioningFailureReason
}

// ResolutionOutcome tags the outcome of a Resolve call so callers can branch
// without parsing the returned wallet's status.
type ResolutionOutcome string

const (
	// ResolutionOutcomeExisting means the phone number is known and maps to
	// an existing wallet record.
	ResolutionOutcomeExisting ResolutionOutcome = "EXISTING"

	// ResolutionOutcomeNeedsNew means the phone number is unknown and a new
	// wallet must be provisioned.
	ResolutionOutcomeNeedsNew ResolutionOutcome = "NEW"
)

// ResolutionResult is returned by WalletResolver.Resolve.
type ResolutionResult struct {
	Outcome ResolutionOutcome
	Wallet  *ParticipantWallet // nil when Outcome == NEW
}

// BatchParticipant is a single entry in a disbursement CSV upload.
type BatchParticipant struct {
	PhoneNumber string
	Name        string
	IDNumber    string
	Amount      string
}

// BatchEligibilityResult is the output of BatchEligibilityFilter.FilterEligible.
type BatchEligibilityResult struct {
	// Eligible contains participants that are Ready and may be included in the
	// disbursement submission.
	Eligible []EligibleParticipant

	// Excluded contains participants that could not be included with a reason.
	// These are never silently dropped.
	Excluded []ExcludedParticipant
}

// EligibleParticipant pairs the original batch row with its resolved wallet.
type EligibleParticipant struct {
	BatchParticipant
	StellarAddress string
}

// ExcludedParticipant pairs the original batch row with a human-readable
// exclusion reason.
type ExcludedParticipant struct {
	BatchParticipant
	Reason string
}

// DefaultAccountStartingBalance is the XLM amount the treasury funds each new
// account with. It covers the base reserve for the account itself, the reserve
// for the trustline, plus enough for a few operations' worth of fee.
// The exact value is a configuration decision; this is the test baseline.
const DefaultAccountStartingBalance = "2.5"
