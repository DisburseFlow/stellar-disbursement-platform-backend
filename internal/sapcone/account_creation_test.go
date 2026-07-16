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
