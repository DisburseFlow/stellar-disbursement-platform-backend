package sapcone

import (
	"context"
	"errors"
	"fmt"

	"github.com/shopspring/decimal"
	"github.com/stellar/go-stellar-sdk/keypair"
	"github.com/stellar/go-stellar-sdk/support/log"
	"github.com/stellar/go-stellar-sdk/txnbuild"
)

// ProvisionerService drives the two-step (CreateAccount + ChangeTrust) on-chain lifecycle for a single participant.
type ProvisionerService struct {
	store   ParticipantWalletStore
	stellar StellarClient
	opts    ProvisionerOptions
}

// NewProvisionerService creates a ProvisionerService with the given dependencies.
func NewProvisionerService(store ParticipantWalletStore, stellar StellarClient, opts ProvisionerOptions) *ProvisionerService {
	return &ProvisionerService{store: store, stellar: stellar, opts: opts}
}

// Provision ensures the participant identified by phoneNumber has an on-chain
// account with a trustline to asset.
//
// Behaviour by current state:
//   - Not found:  creates wallet record, creates account, establishes trustline → Ready
//   - Pending:    resumes provisioning from the beginning (safe to retry)
//   - Ready:      no-op; returns ProvisioningResult{AlreadyReady: true}
//   - Failed:     returns the recorded failure as an error
func (s *ProvisionerService) Provision(ctx context.Context, phoneNumber string, asset ProvisioningAsset, treasury TreasuryAccountInfo) (ProvisioningResult, error) {
	log.Ctx(ctx).Infof("sapcone/provisioner: starting provisioning for phone=%s asset=%s/%s",
		phoneNumber, asset.Code, asset.Issuer)

	wallet, err := s.resolveOrCreateWallet(ctx, phoneNumber)
	if err != nil {
		return ProvisioningResult{}, err
	}

	// Already Ready → idempotent no-op.
	if wallet.Status == ParticipantWalletStatusReady {
		log.Ctx(ctx).Infof("sapcone/provisioner: phone=%s wallet=%s already READY — skipping (idempotent)",
			phoneNumber, wallet.ID)
		return ProvisioningResult{
			WalletID:       wallet.ID,
			StellarAddress: wallet.StellarAddress,
			AlreadyReady:   true,
		}, nil
	}

	// Already Failed → surface the recorded reason; caller decides on retry.
	if wallet.Status == ParticipantWalletStatusFailed {
		log.Ctx(ctx).Warnf("sapcone/provisioner: phone=%s wallet=%s is FAILED (reason=%s) — not retrying",
			phoneNumber, wallet.ID, wallet.FailureReason)
		return ProvisioningResult{
			WalletID:      wallet.ID,
			FailureReason: wallet.FailureReason,
		}, fmt.Errorf("participant wallet is in failed state: %s", wallet.FailureReason)
	}

	// wallet.Status == Pending — proceed with provisioning.
	return s.provisionPending(ctx, wallet, asset, treasury)
}
