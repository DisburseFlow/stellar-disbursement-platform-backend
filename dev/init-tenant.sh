#!/bin/bash
# Initialize tenant and admin user inside the sdp-api container

set -e

CONTAINER_NAME="sdp-api"
PROJECT_NAME="${COMPOSE_PROJECT_NAME:-sdp-testnet}"

echo "=========================================="
echo "  Initializing tenant and admin user"
echo "=========================================="

# Run migrations and tenant setup - use host.docker.internal for DB access from container
docker compose -p "$PROJECT_NAME" --env-file .env exec -T "$CONTAINER_NAME" sh -c "
  ./stellar-disbursement-platform db admin migrate up &&
  ./stellar-disbursement-platform db tss migrate up &&
  ./stellar-disbursement-platform db auth migrate up --all &&
  ./stellar-disbursement-platform db sdp migrate up --all &&
  ./stellar-disbursement-platform db setup-for-network --all &&
  ./stellar-disbursement-platform tenants ensure-default \
    --sdp-ui-base-url http://localhost:3000 \
    --database-url \"postgres://postgres@host.docker.internal:5432/sdp_mtn?sslmode=disable\" \
    --default-tenant-owner-email default@default.local \
    --default-tenant-owner-first-name Default \
    --default-tenant-owner-last-name Owner \
    --distribution-public-key \"\$DISTRIBUTION_PUBLIC_KEY\" \
    --distribution-seed \"\$DISTRIBUTION_SEED\" \
    --network-passphrase \"\$NETWORK_PASSPHRASE\" \
    --horizon-url \"\$HORIZON_URL\" \
    --default-tenant-distribution-account-type DISTRIBUTION_ACCOUNT.STELLAR.ENV \
    --distribution-account-encryption-passphrase \"\$DISTRIBUTION_SEED\" \
    --channel-account-encryption-passphrase \"\$DISTRIBUTION_SEED\" \
    --disable-mfa \"\$DISABLE_MFA\" \
    --disable-recaptcha \"\$DISABLE_RECAPTCHA\"
"

echo "Tenant initialized successfully!"

# Get tenant ID and create admin user
echo "Creating admin user..."
TENANT_ID=$(docker compose -p "$PROJECT_NAME" --env-file .env exec -T "$CONTAINER_NAME" sh -c "
  ./stellar-disbursement-platform tenants list --database-url \"postgres://postgres@host.docker.internal:5432/sdp_mtn?sslmode=disable\" 2>/dev/null | grep default | awk '{print \$1}'
")

if [ -n "$TENANT_ID" ]; then
  echo "Password123!" | docker compose -p "$PROJECT_NAME" --env-file .env exec -T "$CONTAINER_NAME" sh -c "
    ./stellar-disbursement-platform auth add-user owner@default.local Default Owner \
      --password \
      --owner \
      --roles owner \
      --tenant-id $TENANT_ID \
      --database-url \"postgres://postgres@host.docker.internal:5432/sdp_mtn?sslmode=disable\"
  " && echo "Admin user created!" || echo "Admin user may already exist, continuing..."
else
  echo "Warning: Could not find default tenant ID, skipping admin user creation"
fi

echo "=========================================="
echo "  Initialization complete!"
echo "=========================================="