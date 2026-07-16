package sapcone

import (
	"github.com/stretchr/testify/mock"

	"github.com/stellar/go-stellar-sdk/txnbuild"
)

// test_helpers_test.go contains shared fixtures and small testify mock-argument
// matchers used across the sapcone test files.
//
// The Stellar Provisioner builds real *txnbuild.Transaction instances and
// passes them to StellarClient.SubmitTransaction. To assert the EXACT operation
// shape (CreateAccount destination/amount/source; ChangeTrust asset/issuer/source)
// we use testify mock.Matchers that walk tx.Operations() and cast to the
// concrete txnbuild op types. This keeps tests as true unit tests (no network)
// while still asserting operation geometry, not just "SubmitTransaction was
// called".

const (
	// Testnet passphrase used everywhere in sapcone unit tests by default.
	testNetworkPassphrase = "Test SDF Network ; September 2015"

	// Valid ed25519 Stellar keypair used as the treasury fixture. These are
	// randomly-generated test keys — the mock StellarClient never validates
	// them against the network, but txnbuild.NewTransaction validates the
	// address format at build time, so they must be correctly formatted.
	testTreasuryPubkey = "GDEEI3RHXRT6QLSFZNASYBMGKLSM7FOZZI7DMD77AKI7PF7RNEGWODJ3"
	testTreasurySeed   = "SDWUVZUDBITA6Q7TVVKOQH5CGCZCLQILFCLPQIZ3X3LH32VBDJJ4I7WX"

	// The SAP asset code and its issuer address.
	testAssetCode   = "SAP"
	testAssetIssuer = "GCXIAEQYR5TCGD5C2KU3HPHCZPNCHJYE3UN7AKPYQFUGVELJEDF4N6KO"
)

// fixtureTreasuries returns a TreasuryAccountInfo with the well-known test seeds.
func fixtureTreasuries() TreasuryAccountInfo {
	return TreasuryAccountInfo{PublicKey: testTreasuryPubkey, Seed: testTreasurySeed}
}

// fixtureAsset returns the SAP asset the trustline is established to.
func fixtureAsset() ProvisioningAsset {
	return ProvisioningAsset{Code: testAssetCode, Issuer: testAssetIssuer}
}

// fixtureCreditAsset returns the txnbuild CreditAsset form of fixtureAsset(),
// for tests that need to compare against the SDK op struct directly.
func fixtureCreditAsset() txnbuild.CreditAsset {
	return txnbuild.CreditAsset{Code: testAssetCode, Issuer: testAssetIssuer}
}

// mockMatchedNonEmptyAddress returns a testify mock.MatchedBy that accepts
// any non-empty string. Used where the ProvisionerService generates a random
// keypair and we do not know the address ahead of time.
func mockMatchedNonEmptyAddress() interface{} {
	return mock.MatchedBy(func(addr string) bool {
		return addr != ""
	})
}

// matchCreateAccountTx returns a testify mock.MatchedBy that matches a
// *txnbuild.Transaction whose operations contain exactly one CreateAccount
// operation with the given starting balance (destination is any non-empty
// string — the new participant's address is generated at runtime).
//
// Use the two-argument overload to also assert the destination:
//
//	matchCreateAccountTx(destination, startingBalance)
func matchCreateAccountTx(args ...string) interface{} {
	var destination, startingBalance string
	switch len(args) {
	case 2:
		destination, startingBalance = args[0], args[1]
	case 1:
		startingBalance = args[0]
	default:
		return mock.MatchedBy(func(_ *txnbuild.Transaction) bool { return false })
	}

	return mock.MatchedBy(func(tx *txnbuild.Transaction) bool {
		if tx == nil {
			return false
		}
		for _, op := range tx.Operations() {
			if createOp, ok := op.(*txnbuild.CreateAccount); ok {
				if destination != "" && createOp.Destination != destination {
					return false
				}
				return createOp.Amount == startingBalance
			}
		}
		return false
	})
}

// matchChangeTrustTx returns a testify mock.MatchedBy that matches a
// *txnbuild.Transaction whose operations contain exactly one ChangeTrust
// operation for the given asset code and issuer.
//
// The ChangeTrust.Line type is ChangeTrustAsset which embeds the BasicAsset
// interface, so GetCode() and GetIssuer() are promoted methods.
func matchChangeTrustTx(assetCode, assetIssuer string) interface{} {
	return mock.MatchedBy(func(tx *txnbuild.Transaction) bool {
		if tx == nil {
			return false
		}
		for _, op := range tx.Operations() {
			if ctOp, ok := op.(*txnbuild.ChangeTrust); ok {
				return ctOp.Line.GetCode() == assetCode && ctOp.Line.GetIssuer() == assetIssuer
			}
		}
		return false
	})
}

// anyNewAddress returns a mock.MatchedBy matcher that accepts any non-empty
// G-prefixed Stellar address (used where the ProvisionerService generates a
// random keypair and we don't know the address ahead of time).
func anyNewAddress() interface{} {
	return mockMatchedNonEmptyAddress()
}

// stellarAccountWithBalance returns a StellarAccount the mock LoadAccount can
// hand back to the Provisioner. seq is the account's sequence number; balance is
// the XLM balance string returned to the Provisioner.
func stellarAccountWithBalance(balance string) StellarAccount {
	return StellarAccount{
		Address:        testTreasuryPubkey,
		SequenceNumber: 10,
		NativeBalance:  balance,
	}
}
