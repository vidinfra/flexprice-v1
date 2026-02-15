// fix_missed_overage_billing.go
// This script fixes missed overage billing for subscriptions where the
// calculated usage cost doesn't match the invoiced/debited amount.
//
// Use cases:
// - Events processed before TIERED billing support (cost=0)
// - Any mismatch between usage cost and wallet debits
// - Backfilling overage invoices after fixes
//
// Usage:
//
//	go run scripts/main.go -cmd fix-missed-overage \
//	  -tenant-id <tenant_id> \
//	  -environment-id <env_id> \
//	  -subscription-id <sub_id> \
//	  -dry-run true
package internal

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/clickhouse"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	chRepo "github.com/flexprice/flexprice/internal/repository/clickhouse"
	entRepo "github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/sentry"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"

	webhookPublisher "github.com/flexprice/flexprice/internal/webhook/publisher"
)

// noopWebhookPublisher is a no-op implementation of WebhookPublisher for scripts
type noopWebhookPublisher struct{}

func (n *noopWebhookPublisher) PublishWebhook(ctx context.Context, event *types.WebhookEvent) error {
	return nil
}

func (n *noopWebhookPublisher) Close() error {
	return nil
}

// Ensure noopWebhookPublisher implements WebhookPublisher
var _ webhookPublisher.WebhookPublisher = (*noopWebhookPublisher)(nil)

// FixMissedOverageBillingParams holds parameters for fixing missed overage billing
type FixMissedOverageBillingParams struct {
	TenantID       string
	EnvironmentID  string
	SubscriptionID string
	DryRun         bool
}

// FixMissedOverageBillingScript holds all dependencies for the script
type FixMissedOverageBillingScript struct {
	log           *logger.Logger
	serviceParams service.ServiceParams
}

// FixMissedOverageBilling fixes missed overage billing for a subscription
// It calculates the correct total usage cost, compares with already invoiced amount,
// and creates missing overage invoices + wallet debits
func FixMissedOverageBilling(params FixMissedOverageBillingParams) error {
	if params.TenantID == "" || params.EnvironmentID == "" || params.SubscriptionID == "" {
		return fmt.Errorf("TenantID, EnvironmentID, and SubscriptionID are required")
	}

	// Initialize the script with all dependencies
	script, err := newFixMissedOverageBillingScript()
	if err != nil {
		return fmt.Errorf("failed to initialize script: %w", err)
	}

	log.Printf("Starting fix for missed overage billing")
	log.Printf("  Tenant ID: %s", params.TenantID)
	log.Printf("  Environment ID: %s", params.EnvironmentID)
	log.Printf("  Subscription ID: %s", params.SubscriptionID)
	log.Printf("  Dry Run: %v", params.DryRun)

	// Create context with tenant and environment
	ctx := context.Background()
	ctx = context.WithValue(ctx, types.CtxTenantID, params.TenantID)
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, params.EnvironmentID)

	// Step 1: Get subscription details
	subscriptionService := service.NewSubscriptionService(script.serviceParams)
	subscription, err := subscriptionService.GetSubscription(ctx, params.SubscriptionID)
	if err != nil {
		return fmt.Errorf("failed to get subscription: %w", err)
	}

	log.Printf("\nSubscription Details:")
	log.Printf("  Customer ID: %s", subscription.CustomerID)
	log.Printf("  Status: %s", subscription.SubscriptionStatus)
	log.Printf("  Current Period: %s to %s",
		subscription.CurrentPeriodStart.Format(time.RFC3339),
		subscription.CurrentPeriodEnd.Format(time.RFC3339))

	// Step 2: Get total usage cost from subscription usage API
	usageReq := &dto.GetUsageBySubscriptionRequest{
		SubscriptionID: params.SubscriptionID,
	}
	usageResp, err := subscriptionService.GetUsageBySubscription(ctx, usageReq)
	if err != nil {
		return fmt.Errorf("failed to get subscription usage: %w", err)
	}

	totalUsageCost := decimal.NewFromFloat(usageResp.Amount)
	log.Printf("\nUsage Cost Calculation:")
	log.Printf("  Total Usage Cost: $%.2f", usageResp.Amount)
	for _, charge := range usageResp.Charges {
		log.Printf("    - %s: $%.2f (qty: %.0f)",
			charge.MeterDisplayName,
			charge.Amount,
			charge.Quantity)
	}

	// Step 3: Get overage billing config (threshold)
	overageBillingService := service.NewOverageBillingService(script.serviceParams, nil)
	overageConfig, err := overageBillingService.GetOverageBillingConfig(ctx)
	if err != nil {
		log.Printf("Warning: Failed to get overage billing config, using default $5 threshold")
		overageConfig = &types.OverageBillingConfig{
			Enabled:          true,
			InvoiceThreshold: decimal.NewFromFloat(5.0),
		}
	}
	threshold := overageConfig.InvoiceThreshold
	log.Printf("  Invoice Threshold: $%.2f", threshold.InexactFloat64())

	// Step 4: Calculate how many invoices SHOULD exist
	targetBuckets := totalUsageCost.Div(threshold).Floor().IntPart()
	log.Printf("  Target Buckets (total invoices that should exist): %d", targetBuckets)

	// Step 5: Get already invoiced buckets
	invoiceService := service.NewInvoiceService(script.serviceParams)

	// Calculate period ID for the current period
	periodID, err := types.CalculatePeriodID(
		time.Now(),
		subscription.StartDate,
		subscription.CurrentPeriodStart,
		subscription.CurrentPeriodEnd,
		subscription.BillingAnchor,
		subscription.BillingPeriodCount,
		subscription.BillingPeriod,
	)
	if err != nil {
		return fmt.Errorf("failed to calculate period ID: %w", err)
	}

	// Get existing overage invoices for this subscription/period
	invoicedBuckets, err := getInvoicedBuckets(ctx, script.serviceParams, params.SubscriptionID, periodID)
	if err != nil {
		return fmt.Errorf("failed to get invoiced buckets: %w", err)
	}

	existingInvoiceCount := len(invoicedBuckets)
	log.Printf("  Existing Overage Invoices: %d", existingInvoiceCount)

	// Step 6: Calculate missing buckets
	missingBuckets := []int64{}
	for bucket := int64(1); bucket <= targetBuckets; bucket++ {
		if !invoicedBuckets[bucket] {
			missingBuckets = append(missingBuckets, bucket)
		}
	}

	if len(missingBuckets) == 0 {
		log.Printf("\nNo missing invoices. All overage has been properly billed.")
		return nil
	}

	log.Printf("\nMissing Invoices:")
	log.Printf("  Missing Buckets: %v", missingBuckets)
	log.Printf("  Missing Amount: $%.2f", float64(len(missingBuckets))*threshold.InexactFloat64())

	if params.DryRun {
		log.Printf("\n[DRY RUN] Would create %d invoices totaling $%.2f",
			len(missingBuckets), float64(len(missingBuckets))*threshold.InexactFloat64())
		return nil
	}

	// Step 7: Create missing invoices and process wallet payments
	log.Printf("\nCreating missing invoices...")

	walletPaymentService := service.NewWalletPaymentService(script.serviceParams)
	createdCount := 0
	totalDebited := decimal.Zero

	for _, bucket := range missingBuckets {
		idempotencyKey := fmt.Sprintf("overage_%s_%d_%d", params.SubscriptionID, periodID, bucket)

		// Check if invoice already exists (idempotency)
		existingInvoice, err := script.serviceParams.InvoiceRepo.GetByIdempotencyKey(ctx, idempotencyKey)
		if err == nil && existingInvoice != nil {
			log.Printf("  Bucket %d: Invoice already exists (id: %s)", bucket, existingInvoice.ID)
			continue
		}

		// Create overage invoice
		now := time.Now().UTC()
		invoiceReq := dto.CreateInvoiceRequest{
			IdempotencyKey: lo.ToPtr(idempotencyKey),
			CustomerID:     subscription.CustomerID,
			SubscriptionID: lo.ToPtr(params.SubscriptionID),
			InvoiceType:    types.InvoiceTypeOneOff,
			BillingReason:  types.InvoiceBillingReasonManual,
			Currency:       subscription.Currency,
			AmountDue:      threshold,
			AmountPaid:     lo.ToPtr(decimal.Zero),
			PaymentStatus:  lo.ToPtr(types.PaymentStatusPending),
			Total:          threshold,
			Subtotal:       threshold,
			PeriodStart:    lo.ToPtr(subscription.CurrentPeriodStart),
			PeriodEnd:      lo.ToPtr(now),
			Description:    "Real-time overage charges (backfill)",
			LineItems: []dto.CreateInvoiceLineItemRequest{
				{
					DisplayName: lo.ToPtr("Overage Charges"),
					Amount:      threshold,
					Quantity:    decimal.NewFromInt(1),
					PriceType:   lo.ToPtr(string(types.PRICE_TYPE_USAGE)),
					PeriodStart: lo.ToPtr(subscription.CurrentPeriodStart),
					PeriodEnd:   lo.ToPtr(now),
					Metadata: types.Metadata{
						"billing_type":    "overage",
						"subscription_id": params.SubscriptionID,
						"idempotency_key": idempotencyKey,
						"backfill":        "true",
					},
				},
			},
			Metadata: types.Metadata{
				"billing_type":    "overage",
				"subscription_id": params.SubscriptionID,
				"idempotency_key": idempotencyKey,
				"period_id":       fmt.Sprintf("%d", periodID),
				"backfill":        "true",
			},
		}

		invoiceResp, err := invoiceService.CreateInvoice(ctx, invoiceReq)
		if err != nil {
			log.Printf("  Bucket %d: Failed to create invoice: %v", bucket, err)
			continue
		}

		// Finalize the invoice
		err = invoiceService.FinalizeInvoice(ctx, invoiceResp.ID)
		if err != nil {
			log.Printf("  Bucket %d: Failed to finalize invoice %s: %v", bucket, invoiceResp.ID, err)
		}

		// Get the invoice for wallet payment
		inv, err := script.serviceParams.InvoiceRepo.Get(ctx, invoiceResp.ID)
		if err != nil {
			log.Printf("  Bucket %d: Failed to get invoice for payment: %v", bucket, err)
			continue
		}

		// Process wallet payment
		options := service.DefaultWalletPaymentOptions()
		options.AllowNegativeBalance = true
		options.AdditionalMetadata = types.Metadata{
			"billing_type": "overage",
			"invoice_id":   inv.ID,
			"backfill":     "true",
		}

		amountPaid, err := walletPaymentService.ProcessInvoicePaymentWithWallets(ctx, inv, options)
		if err != nil {
			log.Printf("  Bucket %d: Failed to process wallet payment: %v", bucket, err)
		} else {
			totalDebited = totalDebited.Add(amountPaid)
		}

		log.Printf("  Bucket %d: Created invoice %s, debited $%.2f from wallet",
			bucket, invoiceResp.ID, amountPaid.InexactFloat64())
		createdCount++
	}

	log.Printf("\nSummary:")
	log.Printf("  Invoices Created: %d", createdCount)
	log.Printf("  Total Debited: $%.2f", totalDebited.InexactFloat64())

	// Get updated wallet balance
	wallets, err := script.serviceParams.WalletRepo.GetWalletsByCustomerID(ctx, subscription.CustomerID)
	if err == nil && len(wallets) > 0 {
		totalBalance := decimal.Zero
		for _, w := range wallets {
			if w.WalletStatus == types.WalletStatusActive {
				totalBalance = totalBalance.Add(w.Balance)
			}
		}
		log.Printf("  New Wallet Balance: $%.2f", totalBalance.InexactFloat64())
	}

	log.Printf("\nFix completed successfully!")
	return nil
}

// getInvoicedBuckets returns a map of bucket numbers that have overage invoices
func getInvoicedBuckets(
	ctx context.Context,
	params service.ServiceParams,
	subscriptionID string,
	periodID uint64,
) (map[int64]bool, error) {
	// Query invoices for this subscription with billing_type=overage
	filter := &types.InvoiceFilter{
		QueryFilter: &types.QueryFilter{
			Status: lo.ToPtr(types.StatusPublished),
			Limit:  lo.ToPtr(1000),
		},
		SubscriptionID: subscriptionID,
		InvoiceType:    types.InvoiceTypeOneOff,
	}

	invoices, err := params.InvoiceRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	// Build map of invoiced bucket numbers
	invoicedBuckets := make(map[int64]bool)
	periodIDStr := fmt.Sprintf("%d", periodID)
	idempotencyPrefix := fmt.Sprintf("overage_%s_%d_", subscriptionID, periodID)

	for _, inv := range invoices {
		if inv.Metadata == nil {
			continue
		}

		// Check billing_type
		billingType, ok := inv.Metadata["billing_type"]
		if !ok || billingType != "overage" {
			continue
		}

		// Check period_id
		if storedPeriodID, ok := inv.Metadata["period_id"]; ok {
			if storedPeriodID != periodIDStr {
				continue
			}
		}

		// Extract bucket number from idempotency key
		keyStr, ok := inv.Metadata["idempotency_key"]
		if !ok {
			continue
		}

		if len(keyStr) > len(idempotencyPrefix) && keyStr[:len(idempotencyPrefix)] == idempotencyPrefix {
			bucketStr := keyStr[len(idempotencyPrefix):]
			var bucket int64
			if _, err := fmt.Sscanf(bucketStr, "%d", &bucket); err == nil {
				invoicedBuckets[bucket] = true
			}
		}
	}

	return invoicedBuckets, nil
}

// Initialize all services and dependencies
func newFixMissedOverageBillingScript() (*FixMissedOverageBillingScript, error) {
	// Load configuration
	cfg, err := config.NewConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Initialize logger
	log, err := logger.NewLogger(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	// Initialize Postgres client
	entClient, err := postgres.NewEntClients(cfg, log)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to postgres: %w", err)
	}
	pgClient := postgres.NewClient(entClient, log, sentry.NewSentryService(cfg, log))
	cacheClient := cache.NewInMemoryCache()

	// Initialize ClickHouse client
	sentryService := sentry.NewSentryService(cfg, log)
	chStore, err := clickhouse.NewClickHouseStore(cfg, sentryService)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to clickhouse: %w", err)
	}

	// Initialize repositories
	customerRepo := entRepo.NewCustomerRepository(pgClient, log, cacheClient)
	subscriptionRepo := entRepo.NewSubscriptionRepository(pgClient, log, cacheClient)
	subscriptionLineItemRepo := entRepo.NewSubscriptionLineItemRepository(pgClient, log, cacheClient)
	meterRepo := entRepo.NewMeterRepository(pgClient, log, cacheClient)
	priceRepo := entRepo.NewPriceRepository(pgClient, log, cacheClient)
	featureRepo := entRepo.NewFeatureRepository(pgClient, log, cacheClient)
	invoiceRepo := entRepo.NewInvoiceRepository(pgClient, log, cacheClient)
	walletRepo := entRepo.NewWalletRepository(pgClient, log, cacheClient)
	settingsRepo := entRepo.NewSettingsRepository(pgClient, log, cacheClient)
	processedEventRepo := chRepo.NewProcessedEventRepository(chStore, log)
	eventRepo := chRepo.NewEventRepository(chStore, log)
	planRepo := entRepo.NewPlanRepository(pgClient, log, cacheClient)
	entitlementRepo := entRepo.NewEntitlementRepository(pgClient, log, cacheClient)
	paymentRepo := entRepo.NewPaymentRepository(pgClient, log, cacheClient)
	priceUnitRepo := entRepo.NewPriceUnitRepository(pgClient, log, cacheClient)
	creditGrantRepo := entRepo.NewCreditGrantRepository(pgClient, log, cacheClient)
	addonRepo := entRepo.NewAddonRepository(pgClient, log, cacheClient)
	addonAssociationRepo := entRepo.NewAddonAssociationRepository(pgClient, log, cacheClient)
	couponRepo := entRepo.NewCouponRepository(pgClient, log, cacheClient)
	couponAssociationRepo := entRepo.NewCouponAssociationRepository(pgClient, log, cacheClient)
	subscriptionPhaseRepo := entRepo.NewSubscriptionPhaseRepository(pgClient, log, cacheClient)
	taxRateRepo := entRepo.NewTaxRateRepository(pgClient, log, cacheClient)
	taxAssociationRepo := entRepo.NewTaxAssociationRepository(pgClient, log, cacheClient)
	taxAppliedRepo := entRepo.NewTaxAppliedRepository(pgClient, log, cacheClient)
	alertLogsRepo := entRepo.NewAlertLogsRepository(pgClient, log, cacheClient)

	// Create service parameters
	serviceParams := service.ServiceParams{
		Config:                   cfg,
		Logger:                   log,
		DB:                       pgClient,
		CustomerRepo:             customerRepo,
		MeterRepo:                meterRepo,
		PriceRepo:                priceRepo,
		PriceUnitRepo:            priceUnitRepo,
		FeatureRepo:              featureRepo,
		SubRepo:                  subscriptionRepo,
		SubscriptionLineItemRepo: subscriptionLineItemRepo,
		InvoiceRepo:              invoiceRepo,
		WalletRepo:               walletRepo,
		SettingsRepo:             settingsRepo,
		ProcessedEventRepo:       processedEventRepo,
		EventRepo:                eventRepo,
		PlanRepo:                 planRepo,
		EntitlementRepo:          entitlementRepo,
		PaymentRepo:              paymentRepo,
		CreditGrantRepo:          creditGrantRepo,
		AddonRepo:                addonRepo,
		AddonAssociationRepo:     addonAssociationRepo,
		CouponRepo:               couponRepo,
		CouponAssociationRepo:    couponAssociationRepo,
		SubscriptionPhaseRepo:    subscriptionPhaseRepo,
		TaxRateRepo:              taxRateRepo,
		TaxAssociationRepo:       taxAssociationRepo,
		TaxAppliedRepo:           taxAppliedRepo,
		AlertLogsRepo:            alertLogsRepo,
		WebhookPublisher:         &noopWebhookPublisher{},
	}

	return &FixMissedOverageBillingScript{
		log:           log,
		serviceParams: serviceParams,
	}, nil
}

// FixMissedOverageBillingFromEnv runs the script using environment variables
func FixMissedOverageBillingFromEnv() error {
	tenantID := os.Getenv("TENANT_ID")
	environmentID := os.Getenv("ENVIRONMENT_ID")
	subscriptionID := os.Getenv("SUBSCRIPTION_ID")
	dryRun := os.Getenv("DRY_RUN") == "true"

	params := FixMissedOverageBillingParams{
		TenantID:       tenantID,
		EnvironmentID:  environmentID,
		SubscriptionID: subscriptionID,
		DryRun:         dryRun,
	}

	return FixMissedOverageBilling(params)
}
