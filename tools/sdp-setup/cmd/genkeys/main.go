package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/stellar/stellar-disbursement-platform-backend/tools/sdp-setup/internal/accounts"
	"github.com/stellar/stellar-disbursement-platform-backend/internal/utils"
)

func main() {
	envFile := "backend/dev/.env"
	if len(os.Args) > 1 {
		envFile = os.Args[1]
	}

	// Read existing .env
	content, err := os.ReadFile(envFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading %s: %v\n", envFile, err)
		os.Exit(1)
	}

	envContent := string(content)
	
	// Check if keys are already set
	hasDistKey := strings.Contains(envContent, "DISTRIBUTION_PUBLIC_KEY=") && !strings.Contains(envContent, "DISTRIBUTION_PUBLIC_KEY=\n")
	hasDistSeed := strings.Contains(envContent, "DISTRIBUTION_SEED=") && !strings.Contains(envContent, "DISTRIBUTION_SEED=\n")
	hasSep10Key := strings.Contains(envContent, "SEP10_SIGNING_PUBLIC_KEY=") && !strings.Contains(envContent, "SEP10_SIGNING_PUBLIC_KEY=\n")
	hasSep10Seed := strings.Contains(envContent, "SEP10_SIGNING_PRIVATE_KEY=") && !strings.Contains(envContent, "SEP10_SIGNING_PRIVATE_KEY=\n")

	if hasDistKey && hasDistSeed && hasSep10Key && hasSep10Seed {
		fmt.Println("Keys already exist in .env, skipping generation")
		return
	}

	// Generate new keys for testnet
	fmt.Println("Generating new Stellar accounts for testnet...")
	info := accounts.Generate(utils.TestnetNetworkType, "")

	// Update .env content
	replacements := map[string]string{
		"DISTRIBUTION_PUBLIC_KEY=":        "DISTRIBUTION_PUBLIC_KEY=" + info.DistributionPublic,
		"DISTRIBUTION_SEED=":              "DISTRIBUTION_SEED=" + info.DistributionSeed,
		"SEP10_SIGNING_PUBLIC_KEY=":       "SEP10_SIGNING_PUBLIC_KEY=" + info.SEP10Public,
		"SEP10_SIGNING_PRIVATE_KEY=":      "SEP10_SIGNING_PRIVATE_KEY=" + info.SEP10Private,
		"DISTRIBUTION_ACCOUNT_ENCRYPTION_PASSPHRASE=\"${DISTRIBUTION_SEED}\"": "DISTRIBUTION_ACCOUNT_ENCRYPTION_PASSPHRASE=" + info.DistributionSeed,
		"CHANNEL_ACCOUNT_ENCRYPTION_PASSPHRASE=\"${DISTRIBUTION_SEED}\"": "CHANNEL_ACCOUNT_ENCRYPTION_PASSPHRASE=" + info.DistributionSeed,
	}

	for old, new := range replacements {
		envContent = strings.Replace(envContent, old, new, 1)
	}

	// Write back
	err = os.WriteFile(envFile, []byte(envContent), 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error writing %s: %v\n", envFile, err)
		os.Exit(1)
	}

	fmt.Printf("Updated %s with generated keys\n", envFile)
	fmt.Printf("  Distribution Public Key: %s\n", info.DistributionPublic)
	fmt.Printf("  SEP10 Public Key: %s\n", info.SEP10Public)
}