package sapcone

import (
	"context"
	"errors"

	"github.com/stellar/go-stellar-sdk/support/log"
)

// WalletResolverService resolves a phone number to an existing wallet or
// recognises it as new. It performs NO on-chain work — that is the
// ProvisionerService's job.
type WalletResolverService struct {
	store ParticipantWalletStore
}

// NewWalletResolverService creates a WalletResolverService backed by the
// given ParticipantWalletStore.
func NewWalletResolverService(store ParticipantWalletStore) *WalletResolverService {
	return &WalletResolverService{store: store}
}

// Resolve returns ResolutionOutcomeExisting with the wallet when the phone
// number is already known, or ResolutionOutcomeNeedsNew with a nil wallet
// when it is not. Storage errors other than ErrParticipantNotFound are
// propagated. No records are created — that is the Provisioner's job.
func (s *WalletResolverService) Resolve(ctx context.Context, phoneNumber string) (ResolutionResult, error) {
	log.Ctx(ctx).Debugf("sapcone/resolver: resolving phone number %s", phoneNumber)

	wallet, err := s.store.GetByPhoneNumber(ctx, phoneNumber)
	if err != nil {
		if errors.Is(err, ErrParticipantNotFound) {
			log.Ctx(ctx).Debugf("sapcone/resolver: phone number %s not found — outcome NEW", phoneNumber)
			return ResolutionResult{Outcome: ResolutionOutcomeNeedsNew}, nil
		}
		log.Ctx(ctx).Errorf("sapcone/resolver: store error looking up phone number %s: %v", phoneNumber, err)
		return ResolutionResult{}, err
	}

	log.Ctx(ctx).Debugf("sapcone/resolver: phone number %s resolved to existing wallet %s (status=%s)",
		phoneNumber, wallet.ID, wallet.Status)
	return ResolutionResult{Outcome: ResolutionOutcomeExisting, Wallet: wallet}, nil
}
