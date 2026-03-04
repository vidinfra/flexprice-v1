#!/usr/bin/env bash
#
# Reset Stripe customer mappings for a specific FlexPrice environment.
#
# When you switch Stripe test keys, old Stripe customer IDs become invalid.
# This script clears all stripe_customer_id references so FlexPrice
# re-syncs customers to the new Stripe account on next payment.
#
# What it does:
#   1. Lists all customers for the given environment
#   2. Removes stripe_customer_id from each customer's metadata
#   3. Deletes all Stripe entity_integration_mappings for customers
#
# Usage:
#   ./scripts/reset_stripe_customers.sh \
#     --api-url https://api-billing.tenbyte.io \
#     --api-key sk_01KHJKJTBWCZQQJPADYMRFYC3B \
#     --env-id env_01KGHBCS9S28GC7WQAS2T4HD04 \
#     [--dry-run]
#
# Flags:
#   --api-url   FlexPrice API base URL (required)
#   --api-key   FlexPrice API key (required)
#   --env-id    Environment ID to target (required)
#   --dry-run   Print what would be done without making changes

set -euo pipefail

API_URL=""
API_KEY=""
ENV_ID=""
DRY_RUN=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --api-url)  API_URL="$2"; shift 2 ;;
    --api-key)  API_KEY="$2"; shift 2 ;;
    --env-id)   ENV_ID="$2"; shift 2 ;;
    --dry-run)  DRY_RUN=true; shift ;;
    *)          echo "Unknown flag: $1"; exit 1 ;;
  esac
done

if [[ -z "$API_URL" || -z "$API_KEY" || -z "$ENV_ID" ]]; then
  echo "Usage: $0 --api-url <url> --api-key <key> --env-id <environment_id> [--dry-run]"
  exit 1
fi

API_URL="${API_URL%/}" # trim trailing slash

call_api() {
  local method="$1" path="$2"
  shift 2
  curl -s -X "$method" \
    "${API_URL}/v1${path}" \
    -H "x-api-key: ${API_KEY}" \
    -H "X-Environment-ID: ${ENV_ID}" \
    -H "Content-Type: application/json" \
    "$@"
}

echo "=== FlexPrice: Reset Stripe Customer Mappings ==="
echo "API:         ${API_URL}"
echo "Environment: ${ENV_ID}"
echo "Dry run:     ${DRY_RUN}"
echo ""

# --- Step 1: Fetch all customers (paginated) ---
echo "--- Step 1: Fetching customers ---"

OFFSET=0
LIMIT=100
TOTAL_CUSTOMERS=0
UPDATED_CUSTOMERS=0

while true; do
  resp=$(call_api GET "/customers?limit=${LIMIT}&offset=${OFFSET}")

  items=$(echo "$resp" | jq -r '.items // empty')
  if [[ -z "$items" || "$items" == "null" ]]; then
    items=$(echo "$resp" | jq -r '.data // empty')
  fi
  if [[ -z "$items" || "$items" == "null" ]]; then
    echo "No customers found or unexpected response:"
    echo "$resp" | jq . 2>/dev/null || echo "$resp"
    break
  fi

  count=$(echo "$items" | jq 'length')
  if [[ "$count" -eq 0 ]]; then
    break
  fi

  TOTAL_CUSTOMERS=$((TOTAL_CUSTOMERS + count))

  for i in $(seq 0 $((count - 1))); do
    cust_id=$(echo "$items" | jq -r ".[$i].id")
    cust_name=$(echo "$items" | jq -r ".[$i].name // \"(no name)\"")
    stripe_id=$(echo "$items" | jq -r ".[$i].metadata.stripe_customer_id // empty")

    if [[ -n "$stripe_id" ]]; then
      echo "  Customer: ${cust_id} (${cust_name}) -> stripe: ${stripe_id}"

      # Build new metadata without stripe_customer_id
      new_metadata=$(echo "$items" | jq -c ".[$i].metadata | del(.stripe_customer_id)")

      if [[ "$DRY_RUN" == "true" ]]; then
        echo "    [DRY RUN] Would clear stripe_customer_id from metadata"
      else
        update_resp=$(call_api PUT "/customers/${cust_id}" \
          -d "{\"metadata\": ${new_metadata}}")
        err=$(echo "$update_resp" | jq -r '.error // empty')
        if [[ -n "$err" && "$err" != "null" ]]; then
          echo "    ERROR updating customer: ${err}"
        else
          echo "    Cleared stripe_customer_id from metadata"
        fi
      fi

      UPDATED_CUSTOMERS=$((UPDATED_CUSTOMERS + 1))
    fi
  done

  OFFSET=$((OFFSET + LIMIT))

  total=$(echo "$resp" | jq -r '.pagination.total // .total // 0')
  if [[ "$OFFSET" -ge "$total" ]]; then
    break
  fi
done

echo ""
echo "Customers scanned: ${TOTAL_CUSTOMERS}, with Stripe ID: ${UPDATED_CUSTOMERS}"
echo ""

# --- Step 2: Delete Stripe entity integration mappings for customers ---
echo "--- Step 2: Deleting Stripe entity integration mappings ---"

OFFSET=0
DELETED_MAPPINGS=0

while true; do
  resp=$(call_api GET "/entity-integration-mappings?entity_type=customer&provider_types=stripe&limit=${LIMIT}&offset=${OFFSET}")

  items=$(echo "$resp" | jq -r '.items // empty')
  if [[ -z "$items" || "$items" == "null" ]]; then
    items=$(echo "$resp" | jq -r '.data // empty')
  fi
  if [[ -z "$items" || "$items" == "null" ]]; then
    echo "No mappings found or unexpected response."
    break
  fi

  count=$(echo "$items" | jq 'length')
  if [[ "$count" -eq 0 ]]; then
    break
  fi

  for i in $(seq 0 $((count - 1))); do
    mapping_id=$(echo "$items" | jq -r ".[$i].id")
    entity_id=$(echo "$items" | jq -r ".[$i].entity_id")
    provider_entity_id=$(echo "$items" | jq -r ".[$i].provider_entity_id")

    echo "  Mapping: ${mapping_id} (customer: ${entity_id} -> stripe: ${provider_entity_id})"

    if [[ "$DRY_RUN" == "true" ]]; then
      echo "    [DRY RUN] Would delete mapping"
    else
      del_resp=$(call_api DELETE "/entity-integration-mappings/${mapping_id}")
      echo "    Deleted"
    fi

    DELETED_MAPPINGS=$((DELETED_MAPPINGS + 1))
  done

  # Don't increment offset when deleting — items shift down
  if [[ "$DRY_RUN" == "true" ]]; then
    OFFSET=$((OFFSET + LIMIT))
  fi

  total=$(echo "$resp" | jq -r '.pagination.total // .total // 0')
  if [[ "$DRY_RUN" == "true" && "$OFFSET" -ge "$total" ]]; then
    break
  fi
done

echo ""
echo "Stripe mappings deleted: ${DELETED_MAPPINGS}"
echo ""
echo "=== Done ==="
echo "Next payment will re-sync customers to the new Stripe account."
