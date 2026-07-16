package sapcone

import (
	"context"

	"github.com/stellar/go-stellar-sdk/txnbuild"
	"github.com/stretchr/testify/mock"
)

// MockParticipantWalletStore is a testify mock for ParticipantWalletStore.
// It follows the same pattern as the generated mocks in internal/services/mocks/.
type MockParticipantWalletStore struct {
	mock.Mock
}

var _ ParticipantWalletStore = (*MockParticipantWalletStore)(nil)

func (m *MockParticipantWalletStore) GetByPhoneNumber(ctx context.Context, phoneNumber string) (*ParticipantWallet, error) {
	args := m.Called(ctx, phoneNumber)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*ParticipantWallet), args.Error(1)
}

func (m *MockParticipantWalletStore) Create(ctx context.Context, phoneNumber string) (*ParticipantWallet, error) {
	args := m.Called(ctx, phoneNumber)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*ParticipantWallet), args.Error(1)
}

func (m *MockParticipantWalletStore) UpdateStatus(ctx context.Context, id string, status ParticipantWalletStatus, reason ProvisioningFailureReason) error {
	args := m.Called(ctx, id, status, reason)
	return args.Error(0)
}

func (m *MockParticipantWalletStore) UpdateStellarAddress(ctx context.Context, id string, stellarAddress string) error {
	args := m.Called(ctx, id, stellarAddress)
	return args.Error(0)
}

// MockStellarClient is a testify mock for StellarClient.
// All Horizon/network calls in unit tests go through this mock so tests
// never make real network requests.
type MockStellarClient struct {
	mock.Mock
}

var _ StellarClient = (*MockStellarClient)(nil)

func (m *MockStellarClient) LoadAccount(ctx context.Context, address string) (StellarAccount, error) {
	args := m.Called(ctx, address)
	return args.Get(0).(StellarAccount), args.Error(1)
}

func (m *MockStellarClient) SubmitTransaction(ctx context.Context, tx *txnbuild.Transaction, signers ...string) (StellarTransactionResult, error) {
	// Pass signers as a single variadic argument so testify can match it.
	callArgs := []interface{}{ctx, tx}
	for _, s := range signers {
		callArgs = append(callArgs, s)
	}
	args := m.Called(callArgs...)
	return args.Get(0).(StellarTransactionResult), args.Error(1)
}

// MockStellarProvisioner is a testify mock for StellarProvisioner.
// Used in batch-eligibility tests that only need to confirm the filter does
// not call Provision unexpectedly.
type MockStellarProvisioner struct {
	mock.Mock
}

var _ StellarProvisioner = (*MockStellarProvisioner)(nil)

func (m *MockStellarProvisioner) Provision(ctx context.Context, phoneNumber string, asset ProvisioningAsset, treasury TreasuryAccountInfo) (ProvisioningResult, error) {
	args := m.Called(ctx, phoneNumber, asset, treasury)
	return args.Get(0).(ProvisioningResult), args.Error(1)
}

// MockBatchEligibilityFilter is a testify mock for BatchEligibilityFilter.
type MockBatchEligibilityFilter struct {
	mock.Mock
}

var _ BatchEligibilityFilter = (*MockBatchEligibilityFilter)(nil)

func (m *MockBatchEligibilityFilter) FilterEligible(ctx context.Context, batch []BatchParticipant) (BatchEligibilityResult, error) {
	args := m.Called(ctx, batch)
	return args.Get(0).(BatchEligibilityResult), args.Error(1)
}

// MockWalletResolver is a testify mock for WalletResolver.
type MockWalletResolver struct {
	mock.Mock
}

var _ WalletResolver = (*MockWalletResolver)(nil)

func (m *MockWalletResolver) Resolve(ctx context.Context, phoneNumber string) (ResolutionResult, error) {
	args := m.Called(ctx, phoneNumber)
	return args.Get(0).(ResolutionResult), args.Error(1)
}
