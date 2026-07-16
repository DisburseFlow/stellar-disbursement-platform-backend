package sapcone

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/stellar/go-stellar-sdk/txnbuild"
)

// Test_CreateAccount_OperationShape asserts the transaction submitted to the
// Stellar network for account creation contains a single CreateAccount
// operation whose destination, amount, and source account match our expected
// values.
func Test_CreateAccount_OperationShape(t *testing.T) {
	const newPhone = "+254800000001"
	store := new(MockParticipantWalletStore)
	store.On("GetByPhoneNumber", context.Background(), newPhone).
		Return(nil, ErrParticipantNotFound)
	store.On("Create", context.Background(), newPhone).
		Return(&ParticipantWallet{ID: "w-shape", PhoneNumber: newPhone, Status: ParticipantWalletStatusPending}, nil)
	store.On("UpdateStellarAddress", context.Background(), "w-shape", mock.AnythingOfType("string")).Return(nil)
	store.On("UpdateStatus", context.Background(), "w-shape",
		ParticipantWalletStatusReady, ProvisioningFailureReasonNone,
	).Return(nil)

	// Capture the exact CreateAccount operation by intercepting the submitted
	// transaction with a callback-style matcher.
	var capturedTx *txnbuild.Transaction
	stellar := new(MockStellarClient)
	stellar.On("LoadAccount", context.Background(), testTreasuryPubkey).
		Return(stellarAccountWithBalance("100000.0000000"), nil)
	stellar.On("SubmitTransaction", context.Background(),
		mock.MatchedBy(func(tx *txnbuild.Transaction) bool {
			if tx == nil || len(tx.Operations()) != 1 {
				return false
			}
			_, ok := tx.Operations()[0].(*txnbuild.CreateAccount)
			if !ok {
				return false
			}
			capturedTx = tx
			return true
		}),
		testTreasurySeed,
	).Return(StellarTransactionResult{Hash: "tx-shape", Successful: true}, nil)

	// Second submit (ChangeTrust): allow any transaction (it will not be the
	// CreateAccount one because the first expectation specifically matches that).
	stellar.On("LoadAccount", context.Background(),
		mock.MatchedBy(func(addr string) bool { return addr != testTreasuryPubkey && addr != "" }),
	).Return(StellarAccount{Address: "GNEW-SHAPE", SequenceNumber: 0, NativeBalance: "2.5000000"}, nil)
	stellar.On("SubmitTransaction", context.Background(),
		mock.MatchedBy(func(tx *txnbuild.Transaction) bool { return true }),
		mock.AnythingOfType("string"),
	).Return(StellarTransactionResult{Hash: "tx-trust-shape", Successful: true}, nil)

	provisioner := NewProvisionerService(store, stellar, ProvisionerOptions{
		NetworkPassphrase: testNetworkPassphrase,
		StartingBalance:   DefaultAccountStartingBalance,
	})
	_, err := provisioner.Provision(context.Background(), newPhone, fixtureAsset(), fixtureTreasuries())
	require.NoError(t, err)

	require.NotNil(t, capturedTx, "the create-account transaction was never submitted")
	ops := capturedTx.Operations()
	require.Len(t, ops, 1, "expected exactly one operation in the create-account tx")

	createOp, ok := ops[0].(*txnbuild.CreateAccount)
	require.True(t, ok, "expected CreateAccount operation, got %T", ops[0])

	assert.Equal(t, DefaultAccountStartingBalance, createOp.Amount,
		"starting balance should match DefaultAccountStartingBalance")
	assert.NotEmpty(t, createOp.Destination, "destination must be the new account address")
	assert.Equal(t, testTreasuryPubkey, capturedTx.SourceAccount().AccountID,
		"transaction must be sourced from the treasury account")

	// The SourceAccount on the operation is unset (empty) because the
	// transaction source is the same as the operation source.
	assert.Empty(t, createOp.SourceAccount,
		"op source account should be empty when it matches tx source")

	store.AssertExpectations(t)
	stellar.AssertExpectations(t)
}

// Test_CreateAccount_InsufficientTreasuryBalance asserts that when the treasury
// account does not have enough XLM to cover the starting balance, the
// Provisioner returns a distinct error and sets the wallet to Failed with
// reason INSUFFICIENT_TREASURY (not a generic failure).
func Test_CreateAccount_InsufficientTreasuryBalance(t *testing.T) {
	const newPhone = "+254800000002"

	store := new(MockParticipantWalletStore)
	store.On("GetByPhoneNumber", context.Background(), newPhone).
		Return(nil, ErrParticipantNotFound)
	store.On("Create", context.Background(), newPhone).
		Return(&ParticipantWallet{ID: "w-poor", PhoneNumber: newPhone, Status: ParticipantWalletStatusPending}, nil)
	store.On("UpdateStatus", context.Background(), "w-poor",
		ParticipantWalletStatusFailed, ProvisioningFailureReasonInsufficientTreasury,
	).Return(nil)

	stellar := new(MockStellarClient)
	// Treasury has only 1 XLM — far below DefaultAccountStartingBalance (2.5).
	stellar.On("LoadAccount", context.Background(), testTreasuryPubkey).
		Return(stellarAccountWithBalance("1.0000000"), nil)
	// SubmitTransaction must never be called.
	stellar.AssertNotCalled(t, "SubmitTransaction", mock.Anything, mock.Anything, mock.Anything)

	provisioner := NewProvisionerService(store, stellar, ProvisionerOptions{
		NetworkPassphrase: testNetworkPassphrase,
		StartingBalance:   DefaultAccountStartingBalance,
	})

	res, err := provisioner.Provision(context.Background(), newPhone, fixtureAsset(), fixtureTreasuries())

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInsufficientTreasuryBalance,
		"must return sentinel ErrInsufficientTreasuryBalance")
	assert.Equal(t, ProvisioningFailureReasonInsufficientTreasury, res.FailureReason)
	assert.Empty(t, res.AccountCreatedTxHash, "no account creation was attempted")

	store.AssertExpectations(t)
	stellar.AssertExpectations(t)
}

// Test_CreateAccount_ExactStartingBalanceConstant: the DefaultAccountStartingBalance
// constant is the value the Provisioner must use. The test pins the exact value so
// any change is intentional.
func Test_CreateAccount_ExactStartingBalanceConstant(t *testing.T) {
	const expected = "2.5"
	assert.Equal(t, expected, DefaultAccountStartingBalance,
		"DefaultAccountStartingBalance pinned; changing requires coordinator review",
	)
}
