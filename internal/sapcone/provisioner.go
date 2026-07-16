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

// createAccount loads the treasury, checks its balance, builds and submits the
// CreateAccount transaction. Returns an error result on any failure.
func (s *ProvisionerService) createAccount(ctx context.Context, wallet *ParticipantWallet, destination string, treasury TreasuryAccountInfo) (ProvisioningResult, error) {
	log.Ctx(ctx).Debugf("sapcone/provisioner: wallet=%s loading treasury account=%s", wallet.ID, treasury.PublicKey)
	treasuryAccount, err := s.stellar.LoadAccount(ctx, treasury.PublicKey)
	if err != nil {
		log.Ctx(ctx).Errorf("sapcone/provisioner: wallet=%s failed to load treasury account=%s: %v",
			wallet.ID, treasury.PublicKey, err)
		s.markFailed(ctx, wallet.ID, ProvisioningFailureReasonAccountCreationFailed)
		return ProvisioningResult{WalletID: wallet.ID, FailureReason: ProvisioningFailureReasonAccountCreationFailed},
			fmt.Errorf("loading treasury account: %w", err)
	}

	if err := s.checkTreasuryBalance(ctx, wallet.ID, treasuryAccount.NativeBalance); err != nil {
		return ProvisioningResult{WalletID: wallet.ID, FailureReason: ProvisioningFailureReasonInsufficientTreasury}, err
	}

	log.Ctx(ctx).Infof("sapcone/provisioner: wallet=%s submitting CreateAccount: destination=%s amount=%s treasury=%s",
		wallet.ID, destination, s.opts.StartingBalance, treasury.PublicKey)

	tx, err := txnbuild.NewTransaction(txnbuild.TransactionParams{
		SourceAccount:        &txnbuild.SimpleAccount{AccountID: treasury.PublicKey, Sequence: treasuryAccount.SequenceNumber},
		IncrementSequenceNum: true,
		Operations: []txnbuild.Operation{
			&txnbuild.CreateAccount{Destination: destination, Amount: s.opts.StartingBalance},
		},
		BaseFee:       txnbuild.MinBaseFee,
		Preconditions: txnbuild.Preconditions{TimeBounds: txnbuild.NewTimeout(300)},
	})
	if err != nil {
		log.Ctx(ctx).Errorf("sapcone/provisioner: wallet=%s failed to build CreateAccount tx: %v", wallet.ID, err)
		return ProvisioningResult{}, fmt.Errorf("building CreateAccount transaction: %w", err)
	}

	result, err := s.stellar.SubmitTransaction(ctx, tx, treasury.Seed)
	if err != nil || !result.Successful {
		log.Ctx(ctx).Errorf("sapcone/provisioner: wallet=%s CreateAccount FAILED: successful=%v err=%v",
			wallet.ID, result.Successful, err)
		s.markFailed(ctx, wallet.ID, ProvisioningFailureReasonAccountCreationFailed)
		return ProvisioningResult{WalletID: wallet.ID, FailureReason: ProvisioningFailureReasonAccountCreationFailed},
			fmt.Errorf("CreateAccount submission failed: %w", err)
	}

	log.Ctx(ctx).Infof("sapcone/provisioner: wallet=%s CreateAccount SUCCESS txHash=%s newAddress=%s",
		wallet.ID, result.Hash, destination)
	return ProvisioningResult{AccountCreatedTxHash: result.Hash}, nil
}

// establishTrustline loads the new account, builds and submits a ChangeTrust
// transaction signed by the new account's own key.
func (s *ProvisionerService) establishTrustline(ctx context.Context, wallet *ParticipantWallet, kp *keypair.Full, asset ProvisioningAsset, createTxHash string) (ProvisioningResult, error) {
	log.Ctx(ctx).Debugf("sapcone/provisioner: wallet=%s loading new account=%s for ChangeTrust sequence",
		wallet.ID, kp.Address())
	newAccount, err := s.stellar.LoadAccount(ctx, kp.Address())
	if err != nil {
		log.Ctx(ctx).Errorf("sapcone/provisioner: wallet=%s failed to load new account for trustline: %v", wallet.ID, err)
		s.markFailed(ctx, wallet.ID, ProvisioningFailureReasonTrustlineEstablishFailed)
		return ProvisioningResult{
			WalletID:             wallet.ID,
			StellarAddress:       kp.Address(),
			AccountCreatedTxHash: createTxHash,
			FailureReason:        ProvisioningFailureReasonTrustlineEstablishFailed,
		}, fmt.Errorf("loading new account for trustline: %w", err)
	}

	log.Ctx(ctx).Infof("sapcone/provisioner: wallet=%s submitting ChangeTrust: account=%s asset=%s/%s",
		wallet.ID, kp.Address(), asset.Code, asset.Issuer)

	tx, err := txnbuild.NewTransaction(txnbuild.TransactionParams{
		SourceAccount:        &txnbuild.SimpleAccount{AccountID: kp.Address(), Sequence: newAccount.SequenceNumber},
		IncrementSequenceNum: true,
		Operations: []txnbuild.Operation{
			&txnbuild.ChangeTrust{
				Line: txnbuild.ChangeTrustAssetWrapper{
					Asset: txnbuild.CreditAsset{Code: asset.Code, Issuer: asset.Issuer},
				},
			},
		},
		BaseFee:       txnbuild.MinBaseFee,
		Preconditions: txnbuild.Preconditions{TimeBounds: txnbuild.NewTimeout(300)},
	})
	if err != nil {
		log.Ctx(ctx).Errorf("sapcone/provisioner: wallet=%s failed to build ChangeTrust tx: %v", wallet.ID, err)
		return ProvisioningResult{}, fmt.Errorf("building ChangeTrust transaction: %w", err)
	}

	result, err := s.stellar.SubmitTransaction(ctx, tx, kp.Seed())
	if err != nil || !result.Successful {
		log.Ctx(ctx).Errorf("sapcone/provisioner: wallet=%s ChangeTrust FAILED: successful=%v err=%v",
			wallet.ID, result.Successful, err)
		s.markFailed(ctx, wallet.ID, ProvisioningFailureReasonTrustlineEstablishFailed)
		return ProvisioningResult{
			WalletID:             wallet.ID,
			StellarAddress:       kp.Address(),
			AccountCreatedTxHash: createTxHash,
			FailureReason:        ProvisioningFailureReasonTrustlineEstablishFailed,
		}, fmt.Errorf("ChangeTrust submission failed: %w", err)
	}

	log.Ctx(ctx).Infof("sapcone/provisioner: wallet=%s ChangeTrust SUCCESS txHash=%s", wallet.ID, result.Hash)
	return ProvisioningResult{
		AccountCreatedTxHash:   createTxHash,
		TrustlineCreatedTxHash: result.Hash,
	}, nil
}

// checkTreasuryBalance returns ErrInsufficientTreasuryBalance (and marks the
// wallet Failed) if the treasury's XLM balance is below StartingBalance.
func (s *ProvisionerService) checkTreasuryBalance(ctx context.Context, walletID string, nativeBalance string) error {
	treasuryBalance, err := decimal.NewFromString(nativeBalance)
	if err != nil {
		return fmt.Errorf("parsing treasury balance %q: %w", nativeBalance, err)
	}
	startingBalance, err := decimal.NewFromString(s.opts.StartingBalance)
	if err != nil {
		return fmt.Errorf("parsing starting balance %q: %w", s.opts.StartingBalance, err)
	}

	log.Ctx(context.Background()).Debugf("sapcone/provisioner: wallet=%s treasury balance=%s required=%s",
		walletID, treasuryBalance.String(), startingBalance.String())

	if treasuryBalance.LessThan(startingBalance) {
		log.Ctx(context.Background()).Errorf(
			"sapcone/provisioner: wallet=%s INSUFFICIENT treasury balance=%s < required=%s",
			walletID, treasuryBalance.String(), startingBalance.String())
		s.markFailed(context.Background(), walletID, ProvisioningFailureReasonInsufficientTreasury)
		return ErrInsufficientTreasuryBalance
	}
	return nil
}

// markFailed is a best-effort helper that records a Failed status in the store.
// Errors from the store are logged but not propagated — the caller's primary
// error takes precedence.
func (s *ProvisionerService) markFailed(ctx context.Context, walletID string, reason ProvisioningFailureReason) {
	if err := s.store.UpdateStatus(ctx, walletID, ParticipantWalletStatusFailed, reason); err != nil {
		log.Ctx(ctx).Errorf("sapcone/provisioner: wallet=%s failed to persist FAILED status (reason=%s): %v",
			walletID, reason, err)
	}
}
