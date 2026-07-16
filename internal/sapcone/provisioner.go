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

// resolveOrCreateWallet looks up the wallet for phoneNumber, creating a new
// Pending record if none exists. Returns an error only for unexpected store
// failures; a not-found result is handled by creating the record.
func (s *ProvisionerService) resolveOrCreateWallet(ctx context.Context, phoneNumber string) (*ParticipantWallet, error) {
	wallet, err := s.store.GetByPhoneNumber(ctx, phoneNumber)
	if err != nil {
		if !errors.Is(err, ErrParticipantNotFound) {
			log.Ctx(ctx).Errorf("sapcone/provisioner: store error looking up phone=%s: %v", phoneNumber, err)
			return nil, fmt.Errorf("looking up participant: %w", err)
		}
		// Not found → create a new Pending record.
		log.Ctx(ctx).Debugf("sapcone/provisioner: phone=%s not found — creating new PENDING wallet record", phoneNumber)
		wallet, err = s.store.Create(ctx, phoneNumber)
		if err != nil {
			log.Ctx(ctx).Errorf("sapcone/provisioner: failed to create wallet record for phone=%s: %v", phoneNumber, err)
			return nil, fmt.Errorf("creating participant wallet: %w", err)
		}
		log.Ctx(ctx).Debugf("sapcone/provisioner: created wallet record id=%s for phone=%s", wallet.ID, phoneNumber)
	} else {
		log.Ctx(ctx).Debugf("sapcone/provisioner: phone=%s wallet=%s found with status=%s",
			phoneNumber, wallet.ID, wallet.Status)
	}
	return wallet, nil
}

// provisionPending runs the full two-step provisioning flow for a wallet that
// is in Pending status: generate keypair → check treasury → CreateAccount →
// persist address → ChangeTrust → mark Ready.
func (s *ProvisionerService) provisionPending(ctx context.Context, wallet *ParticipantWallet, asset ProvisioningAsset, treasury TreasuryAccountInfo) (ProvisioningResult, error) {
	// Step 1: Generate a new Stellar keypair.
	log.Ctx(ctx).Debugf("sapcone/provisioner: wallet=%s generating new Stellar keypair", wallet.ID)
	kp, err := keypair.Random()
	if err != nil {
		log.Ctx(ctx).Errorf("sapcone/provisioner: wallet=%s keypair generation failed: %v", wallet.ID, err)
		s.markFailed(ctx, wallet.ID, ProvisioningFailureReasonAccountCreationFailed)
		return ProvisioningResult{WalletID: wallet.ID, FailureReason: ProvisioningFailureReasonAccountCreationFailed},
			fmt.Errorf("generating keypair: %w", err)
	}
	log.Ctx(ctx).Debugf("sapcone/provisioner: wallet=%s generated keypair address=%s", wallet.ID, kp.Address())

	// Step 2: Load treasury and check balance.
	createResult, err := s.createAccount(ctx, wallet, kp.Address(), treasury)
	if err != nil {
		return createResult, err
	}

	// Step 3: Persist the new Stellar address.
	if err := s.store.UpdateStellarAddress(ctx, wallet.ID, kp.Address()); err != nil {
		log.Ctx(ctx).Errorf("sapcone/provisioner: wallet=%s failed to persist stellar address=%s (account IS on-chain): %v",
			wallet.ID, kp.Address(), err)
		// The account exists on-chain; best-effort transition to Ready so the
		// record is not stuck in Pending while the chain has the account.
		s.markFailed(ctx, wallet.ID, ProvisioningFailureReasonAccountCreationFailed)
		return ProvisioningResult{
			WalletID:             wallet.ID,
			StellarAddress:       kp.Address(),
			AccountCreatedTxHash: createResult.AccountCreatedTxHash,
		}, fmt.Errorf("persisting stellar address (account was created): %w", err)
	}

	// Step 4: Establish the trustline.
	trustResult, err := s.establishTrustline(ctx, wallet, kp, asset, createResult.AccountCreatedTxHash)
	if err != nil {
		return trustResult, err
	}

	// Step 5: Mark the wallet as Ready.
	log.Ctx(ctx).Debugf("sapcone/provisioner: wallet=%s persisting READY status", wallet.ID)
	if err := s.store.UpdateStatus(ctx, wallet.ID, ParticipantWalletStatusReady, ProvisioningFailureReasonNone); err != nil {
		log.Ctx(ctx).Errorf("sapcone/provisioner: wallet=%s failed to persist READY status: %v", wallet.ID, err)
		return ProvisioningResult{}, fmt.Errorf("persisting READY status: %w", err)
	}

	log.Ctx(ctx).Infof("sapcone/provisioner: wallet=%s provisioning COMPLETE address=%s createTx=%s trustTx=%s",
		wallet.ID, kp.Address(), trustResult.AccountCreatedTxHash, trustResult.TrustlineCreatedTxHash)

	return ProvisioningResult{
		WalletID:               wallet.ID,
		StellarAddress:         kp.Address(),
		AccountCreatedTxHash:   trustResult.AccountCreatedTxHash,
		TrustlineCreatedTxHash: trustResult.TrustlineCreatedTxHash,
	}, nil
}
