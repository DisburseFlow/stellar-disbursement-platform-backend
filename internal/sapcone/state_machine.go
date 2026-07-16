package sapcone

import "fmt"

// ParticipantWalletStateMachine validates lifecycle transitions for a
// participant wallet. It reuses the same transition-table pattern as the
// SDP's own internal/data.StateMachine.
//
// Allowed transitions:
//
//	Pending → Ready   (account created + trustline established)
//	Pending → Failed  (any provisioning error)
//	Failed  → Pending (manual retry reset)
type ParticipantWalletStateMachine struct {
	CurrentStatus ParticipantWalletStatus
	transitions   map[ParticipantWalletStatus]map[ParticipantWalletStatus]bool
}

// NewParticipantWalletStateMachine returns a state machine initialised with
// the given current status.
func NewParticipantWalletStateMachine(current ParticipantWalletStatus) *ParticipantWalletStateMachine {
	type edge struct{ from, to ParticipantWalletStatus }
	edges := []edge{
		{ParticipantWalletStatusPending, ParticipantWalletStatusReady},
		{ParticipantWalletStatusPending, ParticipantWalletStatusFailed},
		{ParticipantWalletStatusFailed, ParticipantWalletStatusPending},
	}

	t := make(map[ParticipantWalletStatus]map[ParticipantWalletStatus]bool)
	for _, e := range edges {
		if t[e.from] == nil {
			t[e.from] = make(map[ParticipantWalletStatus]bool)
		}
		t[e.from][e.to] = true
	}

	return &ParticipantWalletStateMachine{CurrentStatus: current, transitions: t}
}

// CanTransitionTo reports whether the current status may move to target.
func (sm *ParticipantWalletStateMachine) CanTransitionTo(target ParticipantWalletStatus) bool {
	return sm.transitions[sm.CurrentStatus][target]
}

// TransitionTo attempts the transition and returns an error if it is not allowed.
func (sm *ParticipantWalletStateMachine) TransitionTo(target ParticipantWalletStatus) error {
	if !sm.CanTransitionTo(target) {
		return fmt.Errorf("cannot transition participant wallet from %s to %s", sm.CurrentStatus, target)
	}
	sm.CurrentStatus = target
	return nil
}
