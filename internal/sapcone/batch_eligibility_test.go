package sapcone

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// BatchEligibilityService is the concrete implementation of
// BatchEligibilityFilter. Constructor signature pinned by these tests:
//	NewBatchEligibilityService(store ParticipantWalletStore) *BatchEligibilityService
// Test_BatchEligibility_ReadyOnly asserts that of N participants, only those
// with Ready wallets are included; Pending participants are excluded with a
// reason, and the overall operation does not fail.
func Test_BatchEligibility_ReadyOnly(t *testing.T) {
	readyPhone := "+254700000100"
	pendingPhone := "+254700000101"

	store := new(MockParticipantWalletStore)
	store.On("GetByPhoneNumber", context.Background(), readyPhone).
		Return(&ParticipantWallet{
			ID: "w-ready-1", PhoneNumber: readyPhone,
			StellarAddress: "GREADY1",
			Status:         ParticipantWalletStatusReady,
		}, nil)
	store.On("GetByPhoneNumber", context.Background(), pendingPhone).
		Return(&ParticipantWallet{
			ID: "w-pending-1", PhoneNumber: pendingPhone,
			StellarAddress: "",
			Status:         ParticipantWalletStatusPending,
		}, nil)

	batch := []BatchParticipant{
		{PhoneNumber: readyPhone, Name: "Alice", IDNumber: "ID-A1", Amount: "50.00"},
		{PhoneNumber: pendingPhone, Name: "Bob", IDNumber: "ID-B1", Amount: "30.00"},
	}

	filter := NewBatchEligibilityService(store)
	result, err := filter.FilterEligible(context.Background(), batch)

	require.NoError(t, err)

	require.Len(t, result.Eligible, 1, "only Alice (Ready) should be eligible")
	assert.Equal(t, readyPhone, result.Eligible[0].PhoneNumber)
	assert.Equal(t, "Alice", result.Eligible[0].Name)
	assert.Equal(t, "GREADY1", result.Eligible[0].StellarAddress)

	require.Len(t, result.Excluded, 1, "Bob (Pending) must be reported as excluded")
	assert.Equal(t, pendingPhone, result.Excluded[0].PhoneNumber)
	assert.Equal(t, "Bob", result.Excluded[0].Name)
	assert.Contains(t, result.Excluded[0].Reason, "PENDING",
		"exclusion reason must reference the wallet status")

	store.AssertExpectations(t)
}

// Test_BatchEligibility_AllReady: when every participant is already Ready,
// Excluded is empty.
func Test_BatchEligibility_AllReady(t *testing.T) {
	store := new(MockParticipantWalletStore)
	store.On("GetByPhoneNumber", context.Background(), "+254700000110").
		Return(&ParticipantWallet{
			ID: "w-rdy-a", PhoneNumber: "+254700000110",
			StellarAddress: "GREADY-A", Status: ParticipantWalletStatusReady,
		}, nil)
	store.On("GetByPhoneNumber", context.Background(), "+254700000111").
		Return(&ParticipantWallet{
			ID: "w-rdy-b", PhoneNumber: "+254700000111",
			StellarAddress: "GREADY-B", Status: ParticipantWalletStatusReady,
		}, nil)

	batch := []BatchParticipant{
		{PhoneNumber: "+254700000110", Name: "Carol"},
		{PhoneNumber: "+254700000111", Name: "Dave"},
	}

	filter := NewBatchEligibilityService(store)
	result, err := filter.FilterEligible(context.Background(), batch)

	require.NoError(t, err)
	require.Len(t, result.Eligible, 2)
	assert.Len(t, result.Excluded, 0)
}

// Test_BatchEligibility_NoneReady: exclude every participant and report them.
func Test_BatchEligibility_NoneReady(t *testing.T) {
	store := new(MockParticipantWalletStore)
	store.On("GetByPhoneNumber", context.Background(), "+254700000120").
		Return(&ParticipantWallet{
			ID: "w-pend-x", PhoneNumber: "+254700000120",
			Status: ParticipantWalletStatusPending,
		}, nil)
	store.On("GetByPhoneNumber", context.Background(), "+254700000121").
		Return(&ParticipantWallet{
			ID: "w-fail-x", PhoneNumber: "+254700000121",
			Status:        ParticipantWalletStatusFailed,
			FailureReason: ProvisioningFailureReasonAccountCreationFailed,
		}, nil)

	batch := []BatchParticipant{
		{PhoneNumber: "+254700000120", Name: "Eve"},
		{PhoneNumber: "+254700000121", Name: "Frank"},
	}

	filter := NewBatchEligibilityService(store)
	result, err := filter.FilterEligible(context.Background(), batch)

	require.NoError(t, err)
	assert.Len(t, result.Eligible, 0)
	require.Len(t, result.Excluded, 2)
	assert.Contains(t, result.Excluded[0].Reason, "PENDING")
	assert.Contains(t, result.Excluded[1].Reason, "FAILED")
}

// Test_BatchEligibility_StoreErrorPropagates: a DB error while resolving a
// participant must propagate as an error from FilterEligible — the excluded
// list is for wallet-status rejections, not DB failures.
func Test_BatchEligibility_StoreErrorPropagates(t *testing.T) {
	store := new(MockParticipantWalletStore)
	store.On("GetByPhoneNumber", context.Background(), "+254700000130").
		Return(nil, errors.New("connection timeout"))

	batch := []BatchParticipant{
		{PhoneNumber: "+254700000130", Name: "Grace"},
	}

	filter := NewBatchEligibilityService(store)
	_, err := filter.FilterEligible(context.Background(), batch)

	require.Error(t, err)
	store.AssertExpectations(t)
}

// Test_BatchEligibility_PendingNotIncludedAlsoNotError: a Pending participant
// must NOT cause FilterEligible to return an error. It is reported in Excluded.
func Test_BatchEligibility_PendingNotIncludedAlsoNotError(t *testing.T) {
	store := new(MockParticipantWalletStore)
	store.On("GetByPhoneNumber", context.Background(), "+254700000140").
		Return(&ParticipantWallet{
			ID: "w-yet-pending", PhoneNumber: "+254700000140",
			Status: ParticipantWalletStatusPending,
		}, nil)

	batch := []BatchParticipant{
		{PhoneNumber: "+254700000140", Name: "Heidi"},
	}

	filter := NewBatchEligibilityService(store)
	result, err := filter.FilterEligible(context.Background(), batch)

	require.NoError(t, err, "a Pending participant must not cause an error")
	assert.Len(t, result.Eligible, 0)
	require.Len(t, result.Excluded, 1)
	assert.Equal(t, "+254700000140", result.Excluded[0].PhoneNumber)
}

// Test_BatchEligibility_ShouldNotSilentlyDrop asserts the empty batch boundary.
func Test_BatchEligibility_EmptyBatch(t *testing.T) {
	store := new(MockParticipantWalletStore)
	filter := NewBatchEligibilityService(store)
	result, err := filter.FilterEligible(context.Background(), []BatchParticipant{})

	require.NoError(t, err)
	assert.Len(t, result.Eligible, 0)
	assert.Len(t, result.Excluded, 0)
}
