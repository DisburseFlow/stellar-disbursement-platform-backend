package sapcone

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// ProvisionerService is the concrete implementation of StellarProvisioner.
// Constructor signature pinned by these tests:
//	NewProvisionerService(store ParticipantWalletStore, stellar StellarClient, opts ProvisionerOptions) *ProvisionerService

func Test_Provisioner_NewParticipantStartsPending(t *testing.T) {
	const newPhone = "+254700000001"
	store, stellar := happyPathMocks(t, newPhone, "w-new")

	provisioner := NewProvisionerService(store, stellar, ProvisionerOptions{
		NetworkPassphrase: testNetworkPassphrase,
		StartingBalance:   DefaultAccountStartingBalance,
	})

	res, err := provisioner.Provision(context.Background(), newPhone, fixtureAsset(), fixtureTreasuries())

	require.NoError(t, err)
	assert.Equal(t, "w-new", res.WalletID)
	store.AssertCalled(t, "Create", context.Background(), newPhone)
}

func Test_Provisioner_SuccessfulProvisionTransitionsToReady(t *testing.T) {
	const newPhone = "+254700000002"
	store, stellar := happyPathMocks(t, newPhone, "w-ready")

	provisioner := NewProvisionerService(store, stellar, ProvisionerOptions{
		NetworkPassphrase: testNetworkPassphrase,
		StartingBalance:   DefaultAccountStartingBalance,
	})

	res, err := provisioner.Provision(context.Background(), newPhone, fixtureAsset(), fixtureTreasuries())

	require.NoError(t, err)
	assert.False(t, res.AlreadyReady)
	assert.NotEmpty(t, res.StellarAddress)
	assert.Equal(t, ProvisioningFailureReasonNone, res.FailureReason)
	assert.NotEmpty(t, res.AccountCreatedTxHash)
	assert.NotEmpty(t, res.TrustlineCreatedTxHash)

	store.AssertExpectations(t)
	stellar.AssertExpectations(t)
}

func Test_Provisioner_AccountCreationFailureTransitionsToFailed(t *testing.T) {
	const newPhone = "+254700000003"

	store := new(MockParticipantWalletStore)
	store.On("GetByPhoneNumber", context.Background(), newPhone).
		Return(nil, ErrParticipantNotFound)
	store.On("Create", context.Background(), newPhone).
		Return(&ParticipantWallet{ID: "w-accfail", PhoneNumber: newPhone, Status: ParticipantWalletStatusPending}, nil)
	store.On("UpdateStatus", context.Background(), "w-accfail",
		ParticipantWalletStatusFailed, ProvisioningFailureReasonAccountCreationFailed,
	).Return(nil)

	stellar := new(MockStellarClient)
	stellar.On("LoadAccount", context.Background(), testTreasuryPubkey).
		Return(stellarAccountWithBalance("100000.0000000"), nil)
	stellar.On("SubmitTransaction", context.Background(),
		matchCreateAccountTx(DefaultAccountStartingBalance),
		testTreasurySeed,
	).Return(StellarTransactionResult{}, errors.New("tx submission failed"))

	provisioner := NewProvisionerService(store, stellar, ProvisionerOptions{
		NetworkPassphrase: testNetworkPassphrase,
		StartingBalance:   DefaultAccountStartingBalance,
	})

	res, err := provisioner.Provision(context.Background(), newPhone, fixtureAsset(), fixtureTreasuries())

	require.Error(t, err)
	assert.Equal(t, ProvisioningFailureReasonAccountCreationFailed, res.FailureReason)
	assert.Empty(t, res.AccountCreatedTxHash)
	store.AssertExpectations(t)
	stellar.AssertExpectations(t)
}

func Test_Provisioner_TrustlineFailureTransitionsToFailed(t *testing.T) {
	const newPhone = "+254700000004"

	store := new(MockParticipantWalletStore)
	store.On("GetByPhoneNumber", context.Background(), newPhone).
		Return(nil, ErrParticipantNotFound)
	store.On("Create", context.Background(), newPhone).
		Return(&ParticipantWallet{ID: "w-tlfail", PhoneNumber: newPhone, Status: ParticipantWalletStatusPending}, nil)
	store.On("UpdateStellarAddress", context.Background(), "w-tlfail", mock.AnythingOfType("string")).Return(nil)
	store.On("UpdateStatus", context.Background(), "w-tlfail",
		ParticipantWalletStatusFailed, ProvisioningFailureReasonTrustlineEstablishFailed,
	).Return(nil)

	stellar := new(MockStellarClient)
	stellar.On("LoadAccount", context.Background(), testTreasuryPubkey).
		Return(stellarAccountWithBalance("100000.0000000"), nil)
	stellar.On("SubmitTransaction", context.Background(),
		matchCreateAccountTx(DefaultAccountStartingBalance),
		testTreasurySeed,
	).Return(StellarTransactionResult{Hash: "tx-create-ok", Successful: true}, nil)
	stellar.On("LoadAccount", context.Background(),
		mock.MatchedBy(func(addr string) bool { return addr != testTreasuryPubkey && addr != "" }),
	).Return(StellarAccount{Address: "GNEW-TL", SequenceNumber: 0, NativeBalance: "2.5000000"}, nil)
	stellar.On("SubmitTransaction", context.Background(),
		matchChangeTrustTx(testAssetCode, testAssetIssuer),
		mock.AnythingOfType("string"),
	).Return(StellarTransactionResult{}, errors.New("trustline rejected"))

	provisioner := NewProvisionerService(store, stellar, ProvisionerOptions{
		NetworkPassphrase: testNetworkPassphrase,
		StartingBalance:   DefaultAccountStartingBalance,
	})

	res, err := provisioner.Provision(context.Background(), newPhone, fixtureAsset(), fixtureTreasuries())

	require.Error(t, err)
	assert.Equal(t, ProvisioningFailureReasonTrustlineEstablishFailed, res.FailureReason)
	assert.NotEmpty(t, res.AccountCreatedTxHash, "account creation succeeded")
	assert.Empty(t, res.TrustlineCreatedTxHash)

	store.AssertNotCalled(t, "UpdateStatus", mock.Anything, mock.Anything,
		ParticipantWalletStatusReady, mock.Anything,
	)
	store.AssertExpectations(t)
	stellar.AssertExpectations(t)
}

func Test_Provisioner_AlreadyReadyIsNoOp(t *testing.T) {
	const readyPhone = "+254700000005"
	readyWallet := &ParticipantWallet{
		ID: "w-ready", PhoneNumber: readyPhone,
		StellarAddress: "GREADYADDR",
		Status:         ParticipantWalletStatusReady,
	}

	store := new(MockParticipantWalletStore)
	store.On("GetByPhoneNumber", context.Background(), readyPhone).
		Return(readyWallet, nil)

	stellar := new(MockStellarClient)

	provisioner := NewProvisionerService(store, stellar, ProvisionerOptions{
		NetworkPassphrase: testNetworkPassphrase,
		StartingBalance:   DefaultAccountStartingBalance,
	})

	res, err := provisioner.Provision(context.Background(), readyPhone, fixtureAsset(), fixtureTreasuries())

	require.NoError(t, err)
	assert.True(t, res.AlreadyReady)
	assert.Equal(t, ProvisioningFailureReasonNone, res.FailureReason)
	stellar.AssertNotCalled(t, "LoadAccount", mock.Anything, mock.Anything)
	stellar.AssertNotCalled(t, "SubmitTransaction", mock.Anything, mock.Anything, mock.Anything)
}

func Test_Provisioner_ReRunForPendingIsIdempotent(t *testing.T) {
	const pendingPhone = "+254700000006"
	pendingWallet := &ParticipantWallet{
		ID: "w-pending", PhoneNumber: pendingPhone,
		StellarAddress: "", Status: ParticipantWalletStatusPending,
	}

	store := new(MockParticipantWalletStore)
	store.On("GetByPhoneNumber", context.Background(), pendingPhone).
		Return(pendingWallet, nil)
	store.On("UpdateStellarAddress", context.Background(), "w-pending", mock.AnythingOfType("string")).Return(nil)
	store.On("UpdateStatus", context.Background(), "w-pending",
		ParticipantWalletStatusReady, ProvisioningFailureReasonNone,
	).Return(nil)

	stellar := new(MockStellarClient)
	stellar.On("LoadAccount", context.Background(), testTreasuryPubkey).
		Return(stellarAccountWithBalance("100000.0000000"), nil)
	stellar.On("SubmitTransaction", context.Background(),
		matchCreateAccountTx(DefaultAccountStartingBalance),
		testTreasurySeed,
	).Return(StellarTransactionResult{Hash: "tx-create", Successful: true}, nil)
	stellar.On("LoadAccount", context.Background(),
		mock.MatchedBy(func(addr string) bool { return addr != testTreasuryPubkey && addr != "" }),
	).Return(StellarAccount{Address: "GNEW-PEND", SequenceNumber: 0, NativeBalance: "2.5000000"}, nil)
	stellar.On("SubmitTransaction", context.Background(),
		matchChangeTrustTx(testAssetCode, testAssetIssuer),
		mock.AnythingOfType("string"),
	).Return(StellarTransactionResult{Hash: "tx-trust", Successful: true}, nil)

	provisioner := NewProvisionerService(store, stellar, ProvisionerOptions{
		NetworkPassphrase: testNetworkPassphrase,
		StartingBalance:   DefaultAccountStartingBalance,
	})

	res, err := provisioner.Provision(context.Background(), pendingPhone, fixtureAsset(), fixtureTreasuries())

	require.NoError(t, err)
	assert.False(t, res.AlreadyReady, "resumed pending — not a no-op")
	store.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
	store.AssertExpectations(t)
	stellar.AssertExpectations(t)
}

func happyPathMocks(t *testing.T, phone, walletID string) (*MockParticipantWalletStore, *MockStellarClient) {
	t.Helper()

	store := new(MockParticipantWalletStore)
	store.On("GetByPhoneNumber", context.Background(), phone).
		Return(nil, ErrParticipantNotFound)
	store.On("Create", context.Background(), phone).
		Return(&ParticipantWallet{ID: walletID, PhoneNumber: phone, Status: ParticipantWalletStatusPending}, nil)
	store.On("UpdateStellarAddress", context.Background(), walletID, mock.AnythingOfType("string")).Return(nil)
	store.On("UpdateStatus", context.Background(), walletID,
		ParticipantWalletStatusReady, ProvisioningFailureReasonNone,
	).Return(nil)

	stellar := new(MockStellarClient)
	stellar.On("LoadAccount", context.Background(), testTreasuryPubkey).
		Return(stellarAccountWithBalance("100000.0000000"), nil)
	stellar.On("SubmitTransaction", context.Background(),
		matchCreateAccountTx(DefaultAccountStartingBalance),
		testTreasurySeed,
	).Return(StellarTransactionResult{Hash: "tx-create-ok", Successful: true}, nil)
	stellar.On("LoadAccount", context.Background(),
		mock.MatchedBy(func(addr string) bool { return addr != testTreasuryPubkey && addr != "" }),
	).Return(StellarAccount{Address: "GNEW-HP", SequenceNumber: 0, NativeBalance: "2.5000000"}, nil)
	stellar.On("SubmitTransaction", context.Background(),
		matchChangeTrustTx(testAssetCode, testAssetIssuer),
		mock.AnythingOfType("string"),
	).Return(StellarTransactionResult{Hash: "tx-trust-ok", Successful: true}, nil)

	return store, stellar
}
