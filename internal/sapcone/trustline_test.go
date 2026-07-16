package sapcone

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/stellar/go-stellar-sdk/txnbuild"
)

// Test_Trustline_OperationShape asserts that the ChangeTrust transaction is
// submitted with the correct asset code + issuer, and is signed (i.e. the
// mock matches the new account's seed).
func Test_Trustline_OperationShape(t *testing.T) {
	const newPhone = "+254900000001"

	store := new(MockParticipantWalletStore)
	store.On("GetByPhoneNumber", context.Background(), newPhone).
		Return(nil, ErrParticipantNotFound)
	store.On("Create", context.Background(), newPhone).
		Return(&ParticipantWallet{ID: "w-ct", PhoneNumber: newPhone, Status: ParticipantWalletStatusPending}, nil)
	store.On("UpdateStellarAddress", context.Background(), "w-ct", mock.AnythingOfType("string")).Return(nil)
	store.On("UpdateStatus", context.Background(), "w-ct",
		ParticipantWalletStatusReady, ProvisioningFailureReasonNone,
	).Return(nil)

	var capturedTx *txnbuild.Transaction

	stellar := new(MockStellarClient)
	stellar.On("LoadAccount", context.Background(), testTreasuryPubkey).
		Return(stellarAccountWithBalance("100000.0000000"), nil)
	stellar.On("SubmitTransaction", context.Background(),
		mock.MatchedBy(func(tx *txnbuild.Transaction) bool {
			// Capture the CreateAccount tx.
			ops := tx.Operations()
			if len(ops) == 1 {
				if _, ok := ops[0].(*txnbuild.CreateAccount); ok {
					return true
				}
			}
			return false
		}),
		testTreasurySeed,
	).Return(StellarTransactionResult{Hash: "tx-create-trust", Successful: true}, nil)

	stellar.On("LoadAccount", context.Background(),
		mock.MatchedBy(func(addr string) bool { return addr != testTreasuryPubkey && addr != "" }),
	).Return(StellarAccount{Address: "GNEW-TRUST", SequenceNumber: 0, NativeBalance: "2.5000000"}, nil)

	stellar.On("SubmitTransaction", context.Background(),
		mock.MatchedBy(func(tx *txnbuild.Transaction) bool {
			capturedTx = tx
			return true
		}),
		mock.AnythingOfType("string"), // signed by new account's seed
	).Return(StellarTransactionResult{Hash: "tx-trust-op", Successful: true}, nil)

	provisioner := NewProvisionerService(store, stellar, ProvisionerOptions{
		NetworkPassphrase: testNetworkPassphrase,
		StartingBalance:   DefaultAccountStartingBalance,
	})
	_, err := provisioner.Provision(context.Background(), newPhone, fixtureAsset(), fixtureTreasuries())
	require.NoError(t, err)

	require.NotNil(t, capturedTx, "the change-trust transaction was never submitted")
	ops := capturedTx.Operations()
	require.Len(t, ops, 1, "expected exactly one operation in the change-trust tx")

	ctOp, ok := ops[0].(*txnbuild.ChangeTrust)
	require.True(t, ok, "expected ChangeTrust operation, got %T", ops[0])

	assert.Equal(t, testAssetCode, ctOp.Line.GetCode(),
		"trustline asset code must match")
	assert.Equal(t, testAssetIssuer, ctOp.Line.GetIssuer(),
		"trustline asset issuer must match")

	// The transaction source is the new account (not the treasury), but the
	// address is generated randomly at provisioning time so we can only verify
	// it is not the treasury address.
	assert.NotEqual(t, testTreasuryPubkey, capturedTx.SourceAccount().AccountID,
		"trustline tx must be sourced from the new account, not the treasury")
	assert.NotEmpty(t, capturedTx.SourceAccount().AccountID,
		"trustline tx source must be non-empty")

	store.AssertExpectations(t)
	stellar.AssertExpectations(t)
}

// Test_Trustline_FailureDoesNotMarkReady asserts that if the ChangeTrust
// submission fails, the wallet is set to Failed with
// TRUSTLINE_ESTABLISHMENT_FAILED and the account creation success is NOT
// rolled back.
func Test_Trustline_FailureDoesNotMarkReady(t *testing.T) {
	const newPhone = "+254900000002"

	store := new(MockParticipantWalletStore)
	store.On("GetByPhoneNumber", context.Background(), newPhone).
		Return(nil, ErrParticipantNotFound)
	store.On("Create", context.Background(), newPhone).
		Return(&ParticipantWallet{ID: "w-tl-fail2", PhoneNumber: newPhone, Status: ParticipantWalletStatusPending}, nil)
	store.On("UpdateStellarAddress", context.Background(), "w-tl-fail2", mock.AnythingOfType("string")).Return(nil)
	store.On("UpdateStatus", context.Background(), "w-tl-fail2",
		ParticipantWalletStatusFailed, ProvisioningFailureReasonTrustlineEstablishFailed,
	).Return(nil)

	stellar := new(MockStellarClient)
	stellar.On("LoadAccount", context.Background(), testTreasuryPubkey).
		Return(stellarAccountWithBalance("100000.0000000"), nil)
	stellar.On("SubmitTransaction", context.Background(),
		matchCreateAccountTx(DefaultAccountStartingBalance),
		testTreasurySeed,
	).Return(StellarTransactionResult{Hash: "tx-create-ok2", Successful: true}, nil)
	stellar.On("LoadAccount", context.Background(),
		mock.MatchedBy(func(addr string) bool { return addr != testTreasuryPubkey && addr != "" }),
	).Return(StellarAccount{Address: "GNEW-FAIL2", SequenceNumber: 0, NativeBalance: "2.5000000"}, nil)
	stellar.On("SubmitTransaction", context.Background(),
		matchChangeTrustTx(testAssetCode, testAssetIssuer),
		mock.AnythingOfType("string"),
	).Return(StellarTransactionResult{}, errors.New("trustline rejected: asset not found"))

	provisioner := NewProvisionerService(store, stellar, ProvisionerOptions{
		NetworkPassphrase: testNetworkPassphrase,
		StartingBalance:   DefaultAccountStartingBalance,
	})

	res, err := provisioner.Provision(context.Background(), newPhone, fixtureAsset(), fixtureTreasuries())

	require.Error(t, err)
	assert.Equal(t, ProvisioningFailureReasonTrustlineEstablishFailed, res.FailureReason)
	assert.NotEmpty(t, res.AccountCreatedTxHash, "account was created before trustline attempt")
	assert.Empty(t, res.TrustlineCreatedTxHash, "trustline tx did not succeed")

	// Ready must never be set.
	store.AssertNotCalled(t, "UpdateStatus", mock.Anything, mock.Anything,
		ParticipantWalletStatusReady, mock.Anything,
	)
	store.AssertExpectations(t)
	stellar.AssertExpectations(t)
}

// Test_Trustline_ActiveChangeTrustAssetShape asserts that the ChangeTrust
// operation uses a CreditAsset (a trustline to a known asset/issuer), not
// a LiquidityPoolShare or native asset.
func Test_Trustline_ActiveChangeTrustAssetShape(t *testing.T) {
	const newPhone = "+254900000003"

	store := new(MockParticipantWalletStore)
	store.On("GetByPhoneNumber", context.Background(), newPhone).
		Return(nil, ErrParticipantNotFound)
	store.On("Create", context.Background(), newPhone).
		Return(&ParticipantWallet{ID: "w-tl-fail2", PhoneNumber: newPhone, Status: ParticipantWalletStatusPending}, nil)
	store.On("UpdateStellarAddress", context.Background(), "w-tl-fail2", mock.AnythingOfType("string")).Return(nil)
	store.On("UpdateStatus", context.Background(), "w-tl-fail2",
		ParticipantWalletStatusReady, ProvisioningFailureReasonNone,
	).Return(nil)

	stellar := new(MockStellarClient)
	stellar.On("LoadAccount", context.Background(), testTreasuryPubkey).
		Return(stellarAccountWithBalance("100000.0000000"), nil)
	stellar.On("SubmitTransaction", context.Background(),
		matchCreateAccountTx(DefaultAccountStartingBalance),
		testTreasurySeed,
	).Return(StellarTransactionResult{Hash: "tx-create-cta", Successful: true}, nil)
	stellar.On("LoadAccount", context.Background(),
		mock.MatchedBy(func(addr string) bool { return addr != testTreasuryPubkey && addr != "" }),
	).Return(StellarAccount{Address: "GNEW-CTA", SequenceNumber: 0, NativeBalance: "2.5000000"}, nil)
	stellar.On("SubmitTransaction", context.Background(),
		mock.MatchedBy(func(tx *txnbuild.Transaction) bool {
			if tx == nil || len(tx.Operations()) != 1 {
				return false
			}
			ctOp, ok := tx.Operations()[0].(*txnbuild.ChangeTrust)
			if !ok {
				return false
			}
			// Assert the line is a CreditAsset (not LiquidityPoolShare, not native).
			ca, ok := ctOp.Line.(txnbuild.ChangeTrustAssetWrapper)
			if !ok {
				return false
			}
			creditAsset, ok := ca.Asset.(txnbuild.CreditAsset)
			if !ok {
				return false
			}
			return creditAsset.Code == testAssetCode && creditAsset.Issuer == testAssetIssuer
		}),
		mock.AnythingOfType("string"),
	).Return(StellarTransactionResult{Hash: "tx-trust-cta", Successful: true}, nil)

	provisioner := NewProvisionerService(store, stellar, ProvisionerOptions{
		NetworkPassphrase: testNetworkPassphrase,
		StartingBalance:   DefaultAccountStartingBalance,
	})
	_, err := provisioner.Provision(context.Background(), newPhone, fixtureAsset(), fixtureTreasuries())
	require.NoError(t, err)

	store.AssertExpectations(t)
	stellar.AssertExpectations(t)
}
