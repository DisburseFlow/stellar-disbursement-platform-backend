package sapcone

import "errors"

var (
	// ErrParticipantAlreadyExists is returned when an attempt is made to
	// create a second wallet record for an already-known phone number.
	ErrParticipantAlreadyExists = errors.New("participant with this phone number already exists")

	// ErrParticipantNotFound is returned when a lookup finds no record for
	// the given phone number.
	ErrParticipantNotFound = errors.New("participant not found")

	// ErrInsufficientTreasuryBalance is returned by the ProvisionerService
	// when the treasury account cannot cover the minimum reserve + fee
	// required to create a new account.
	ErrInsufficientTreasuryBalance = errors.New("treasury account has insufficient balance to create new account")

	// ErrAlreadyReady is returned by Provision when the participant is
	// already in the Ready state, signalling the caller that the call was
	// a no-op rather than an error.
	ErrAlreadyReady = errors.New("participant wallet is already ready; provisioning is a no-op")
)
