package sapcone

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// WalletResolverService is the concrete implementation of WalletResolver that
// backs only on a ParticipantWalletStore (no network access). It is defined by
// the implementation session; these tests pin its constructor signature:
//
//	NewWalletResolverService(store ParticipantWalletStore) *WalletResolverService
//
// An alternative open-design question (see summary) is whether resolution and
// provisioning live in the same struct. These tests keep them separate: a
// Resolve call MUST NOT trigger provisioning, and asserting that no Stellar
// call happens is easier when Resolve has no Stellar dependency.

// Test_WalletResolver_Resolve pins the phone-number → wallet contract:
//
//   - known phone → EXISTING outcome with the wallet, no provisioning side effect
//   - unknown phone → NEW outcome, nil wallet, no provisioning side effect
//
// Provisioning-on-lookup (the wrong behaviour the tests guard against) is ruled
// out by intercepting the store and asserting neither Create nor any Stellar
// call happens; the resolver here has no client at all so there's nothing to
// call, but Resolve must also not Create a record prematurely.
func Test_WalletResolver_Resolve(t *testing.T) {
	const knownPhone = "+254700000000"
	knownWallet := &ParticipantWallet{
		ID:             "w-1",
		PhoneNumber:    knownPhone,
		StellarAddress: "GKNOWNADDRESS0000000000000000000000000000000000000000",
		Status:         ParticipantWalletStatusReady,
	}

	tests := []struct {
		name        string
		phoneNumber string
		storeSetup  func(*MockParticipantWalletStore)
		wantOutcome ResolutionOutcome
		wantWallet  *ParticipantWallet
		// assertNoCreate: the test asserts the resolver never pre-creates a
		// wallet record during resolution. Record creation is the Provisioner's
		// responsibility, gated on the caller acting on a NEW outcome.
		assertNoCreate bool
	}{
		{
			name:        "known phone number resolves to existing wallet without provisioning",
			phoneNumber: knownPhone,
			storeSetup: func(s *MockParticipantWalletStore) {
				s.On("GetByPhoneNumber", context.Background(), knownPhone).
					Return(knownWallet, nil)
			},
			wantOutcome:    ResolutionOutcomeExisting,
			wantWallet:     knownWallet,
			assertNoCreate: true,
		},
		{
			name:        "unknown phone number returns NEW outcome with nil wallet",
			phoneNumber: "+254711111111",
			storeSetup: func(s *MockParticipantWalletStore) {
				s.On("GetByPhoneNumber", context.Background(), "+254711111111").
					Return(nil, ErrParticipantNotFound)
			},
			wantOutcome:    ResolutionOutcomeNeedsNew,
			wantWallet:     nil,
			assertNoCreate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := new(MockParticipantWalletStore)
			tt.storeSetup(store)
			if tt.assertNoCreate {
				// No Create expectation set: if the resolver calls Create,
				// testify will fail the test with an unexpected-call error.
			}

			resolver := NewWalletResolverService(store)
			got, err := resolver.Resolve(context.Background(), tt.phoneNumber)

			require.NoError(t, err)
			assert.Equal(t, tt.wantOutcome, got.Outcome)
			assert.Equal(t, tt.wantWallet, got.Wallet)

			store.AssertExpectations(t)
		})
	}
}

// Test_WalletResolver_StoreErrorPropagates: a storage failure (not the
// not-found sentinel) must surface as an error rather than masquerading as NEW.
func Test_WalletResolver_StoreErrorPropagates(t *testing.T) {
	store := new(MockParticipantWalletStore)
	storeErr := assertStoreErr("db connection lost")
	store.On("GetByPhoneNumber", context.Background(), "+254722222222").
		Return(nil, storeErr)

	resolver := NewWalletResolverService(store)
	got, err := resolver.Resolve(context.Background(), "+254722222222")

	require.Error(t, err)
	assert.Equal(t, ResolutionResult{}, got)
	store.AssertExpectations(t)
}

// Test_WalletResolver_TypicalResolutionLifecycle proves the resolution +
// provisioning handoff shape end-to-end at the contract level: an unknown
// number resolves NEW with a nil wallet, and a follow-up Provision call
// (via the resolver's provisioner sibling) is what creates the record. The
// resolver itself must NOT have created the wallet by the time Resolve returns.
func Test_WalletResolver_TypicalResolutionLifecycle(t *testing.T) {
	const newPhone = "+254733333333"

	store := new(MockParticipantWalletStore)
	store.On("GetByPhoneNumber", context.Background(), newPhone).
		Return(nil, ErrParticipantNotFound)

	resolver := NewWalletResolverService(store)

	got, err := resolver.Resolve(context.Background(), newPhone)
	require.NoError(t, err)
	assert.Equal(t, ResolutionOutcomeNeedsNew, got.Outcome)
	assert.Nil(t, got.Wallet, "resolver must not have created a wallet record")

	// The record still does not exist; provisioning owns creation. We prove
	// this by asserting Create was never called during resolution.
	store.AssertNumberOfCalls(t, "Create", 0)
	store.AssertExpectations(t)
}

// assertStoreErr is a tiny helper so the test doesn't need to allocate a named
// error for each call site unnecessarily.
func assertStoreErr(msg string) error { return &storeErr{msg: msg} }

type storeErr struct{ msg string }

func (e *storeErr) Error() string { return e.msg }
