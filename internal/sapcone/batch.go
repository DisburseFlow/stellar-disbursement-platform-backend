package sapcone

import (
	"context"
	"errors"
	"fmt"

	"github.com/stellar/go-stellar-sdk/support/log"
)

// BatchEligibilityService determines which participants in a batch may proceed
// to disbursement based on their wallet provisioning status.
type BatchEligibilityService struct {
	store ParticipantWalletStore
}

// NewBatchEligibilityService creates a BatchEligibilityService backed by the
// given ParticipantWalletStore.
func NewBatchEligibilityService(store ParticipantWalletStore) *BatchEligibilityService {
	return &BatchEligibilityService{store: store}
}

// FilterEligible partitions batch into eligible and excluded sets.
//
// Participants whose wallet status is Ready are placed in Eligible.
// All others (Pending, Failed, or unknown) are placed in Excluded with a
// human-readable reason. A returned error means the entire filter failed
// (e.g. DB unavailable), not an individual participant failure.
func (s *BatchEligibilityService) FilterEligible(ctx context.Context, batch []BatchParticipant) (BatchEligibilityResult, error) {
	log.Ctx(ctx).Infof("sapcone/batch: evaluating eligibility for %d participants", len(batch))

	var result BatchEligibilityResult

	for _, p := range batch {
		log.Ctx(ctx).Debugf("sapcone/batch: checking phone=%s", p.PhoneNumber)

		eligible, excluded, err := s.classify(ctx, p)
		if err != nil {
			log.Ctx(ctx).Errorf("sapcone/batch: store error for phone=%s: %v", p.PhoneNumber, err)
			return BatchEligibilityResult{}, fmt.Errorf("looking up phone number %s: %w", p.PhoneNumber, err)
		}

		if eligible != nil {
			log.Ctx(ctx).Debugf("sapcone/batch: phone=%s ELIGIBLE address=%s", p.PhoneNumber, eligible.StellarAddress)
			result.Eligible = append(result.Eligible, *eligible)
		} else {
			log.Ctx(ctx).Debugf("sapcone/batch: phone=%s EXCLUDED reason=%s", p.PhoneNumber, excluded.Reason)
			result.Excluded = append(result.Excluded, *excluded)
		}
	}

	log.Ctx(ctx).Infof("sapcone/batch: eligibility complete — eligible=%d excluded=%d",
		len(result.Eligible), len(result.Excluded))

	return result, nil
}

// classify checks one participant's wallet status and returns either an
// EligibleParticipant or an ExcludedParticipant. A non-nil error means an
// unexpected store failure (not a not-found).
func (s *BatchEligibilityService) classify(ctx context.Context, p BatchParticipant) (*EligibleParticipant, *ExcludedParticipant, error) {
	wallet, err := s.store.GetByPhoneNumber(ctx, p.PhoneNumber)
	if err != nil {
		if errors.Is(err, ErrParticipantNotFound) {
			return nil, &ExcludedParticipant{
				BatchParticipant: p,
				Reason:           "participant wallet not found — provisioning required",
			}, nil
		}
		return nil, nil, err
	}

	if wallet.Status == ParticipantWalletStatusReady {
		return &EligibleParticipant{
			BatchParticipant: p,
			StellarAddress:   wallet.StellarAddress,
		}, nil, nil
	}

	reason := fmt.Sprintf("participant wallet status is %s", wallet.Status)
	if wallet.Status == ParticipantWalletStatusFailed && wallet.FailureReason != ProvisioningFailureReasonNone {
		reason = fmt.Sprintf("participant wallet failed: %s", wallet.FailureReason)
	}
	return nil, &ExcludedParticipant{BatchParticipant: p, Reason: reason}, nil
}
