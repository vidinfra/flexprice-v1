package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// OverageBillingService handles real-time overage billing
// It detects overage during event processing, accumulates cost per subscription,
// creates invoices when threshold is reached, and deducts from wallet
type OverageBillingService interface {
	// ProcessEventOverage processes an event for overage billing
	// Called after each event's cost is calculated in event post-processing
	ProcessEventOverage(ctx context.Context, processedEvent *events.ProcessedEvent, sub *dto.SubscriptionResponse) error

	// GetOverageBillingConfig retrieves the overage billing configuration for the tenant
	GetOverageBillingConfig(ctx context.Context) (*types.OverageBillingConfig, error)
}

type overageBillingService struct {
	ServiceParams
	processedEventRepo events.ProcessedEventRepository
}

// NewOverageBillingService creates a new overage billing service
func NewOverageBillingService(params ServiceParams, processedEventRepo events.ProcessedEventRepository) OverageBillingService {
	return &overageBillingService{
		ServiceParams:      params,
		processedEventRepo: processedEventRepo,
	}
}

// ProcessEventOverage processes an event for overage billing
func (s *overageBillingService) ProcessEventOverage(
	ctx context.Context,
	processedEvent *events.ProcessedEvent,
	sub *dto.SubscriptionResponse,
) error {
	// 1. Get overage billing config
	config, err := s.GetOverageBillingConfig(ctx)
	if err != nil {
		s.Logger.Warnw("failed to get overage billing config, skipping overage processing",
			"error", err,
			"tenant_id", types.GetTenantID(ctx))
		return nil // Don't fail event processing
	}

	// Check if overage billing is enabled
	if !config.Enabled {
		return nil
	}

	// 2. Check if event has overage cost
	if !s.hasOverageCost(processedEvent) {
		return nil
	}

	// 3. Get total period cost (NOT uninvoiced - we need total for threshold bucket calculation)
	totalPeriodCost, err := s.processedEventRepo.GetPeriodCost(
		ctx,
		types.GetTenantID(ctx),
		types.GetEnvironmentID(ctx),
		sub.CustomerID,
		sub.ID,
		processedEvent.PeriodID,
	)
	if err != nil {
		s.Logger.Errorw("failed to get period cost",
			"error", err,
			"subscription_id", sub.ID,
			"period_id", processedEvent.PeriodID)
		return nil // Don't fail event processing
	}

	// 4. Calculate target threshold bucket (which $5 bucket are we in?)
	// e.g., $0-$5 = bucket 1, $5-$10 = bucket 2, etc.
	targetBucket := totalPeriodCost.Div(config.InvoiceThreshold).IntPart()
	if targetBucket < 1 {
		// Not yet at first threshold
		return nil
	}

	// 5. Get all invoiced buckets to determine which ones are missing
	// This ensures we don't skip any buckets, even if some were missed previously
	invoicedBuckets, err := s.getInvoicedBuckets(ctx, sub.ID, processedEvent.PeriodID)
	if err != nil {
		s.Logger.Errorw("failed to get invoiced buckets",
			"error", err,
			"subscription_id", sub.ID,
			"period_id", processedEvent.PeriodID)
		return nil // Don't fail event processing
	}

	// Count how many buckets are missing
	missingCount := int64(0)
	for bucket := int64(1); bucket <= targetBucket; bucket++ {
		if !invoicedBuckets[bucket] {
			missingCount++
		}
	}

	// If no missing buckets, nothing to do
	if missingCount == 0 {
		s.Logger.Debugw("all buckets already invoiced",
			"subscription_id", sub.ID,
			"period_id", processedEvent.PeriodID,
			"target_bucket", targetBucket)
		return nil
	}

	// 6. Create invoices for all missing buckets from 1 to targetBucket
	invoiceAmount := config.InvoiceThreshold
	var lastCreatedInvoice *invoice.Invoice

	for bucket := int64(1); bucket <= targetBucket; bucket++ {
		// Skip if this bucket already has an invoice
		if invoicedBuckets[bucket] {
			continue
		}
		idempotencyKey := fmt.Sprintf("overage_%s_%d_%d", sub.ID, processedEvent.PeriodID, bucket)

		// Check if invoice already exists for this bucket (idempotency check)
		existingInvoice, err := s.InvoiceRepo.GetByIdempotencyKey(ctx, idempotencyKey)
		if err != nil && !ierr.IsNotFound(err) {
			s.Logger.Errorw("failed to check existing invoice",
				"error", err,
				"idempotency_key", idempotencyKey)
			continue // Try next bucket
		}

		if existingInvoice != nil {
			// Invoice already exists for this bucket, skip to next
			s.Logger.Debugw("overage invoice already exists for bucket",
				"subscription_id", sub.ID,
				"period_id", processedEvent.PeriodID,
				"bucket", bucket,
				"existing_invoice_id", existingInvoice.ID)
			continue
		}

		s.Logger.Infow("overage threshold reached, creating invoice",
			"subscription_id", sub.ID,
			"total_period_cost", totalPeriodCost.String(),
			"bucket", bucket,
			"target_bucket", targetBucket,
			"invoice_amount", invoiceAmount.String(),
			"idempotency_key", idempotencyKey)

		// Create overage invoice with idempotency key
		inv, err := s.createOverageInvoice(ctx, sub, invoiceAmount, idempotencyKey, processedEvent.PeriodID)
		if err != nil {
			// Check if it's a duplicate key error (race condition - another process created it)
			if ierr.IsAlreadyExists(err) {
				s.Logger.Infow("overage invoice already created by another process",
					"subscription_id", sub.ID,
					"idempotency_key", idempotencyKey)
				continue // Try next bucket
			}
			s.Logger.Errorw("failed to create overage invoice",
				"error", err,
				"subscription_id", sub.ID,
				"bucket", bucket,
				"invoice_amount", invoiceAmount.String())
			continue // Try next bucket
		}

		s.Logger.Infow("created overage invoice",
			"invoice_id", inv.ID,
			"subscription_id", sub.ID,
			"bucket", bucket,
			"amount", inv.AmountDue.String())

		// Process wallet payment for this invoice
		_, err = s.processWalletPayment(ctx, inv)
		if err != nil {
			s.Logger.Errorw("failed to process wallet payment for overage invoice",
				"error", err,
				"invoice_id", inv.ID,
				"customer_id", sub.CustomerID)
			// Don't fail - invoice was created, continue to next bucket
		}

		lastCreatedInvoice = inv
	}

	// 7. Check if wallet crossed from positive to negative after all invoices processed
	if lastCreatedInvoice != nil {
		walletBalanceAfter, err := s.getCustomerWalletBalance(ctx, sub.CustomerID)
		if err != nil {
			s.Logger.Errorw("failed to get wallet balance after payment",
				"error", err,
				"customer_id", sub.CustomerID)
			return nil
		}

		// Only send webhook if balance is now negative
		// Note: We can't easily track "crossing" anymore since we process multiple invoices
		// So we just send the webhook if balance is negative and this is a new invoice
		if walletBalanceAfter.LessThan(decimal.Zero) {
			s.Logger.Infow("wallet has negative balance after overage billing",
				"customer_id", sub.CustomerID,
				"subscription_id", sub.ID,
				"balance", walletBalanceAfter.String())

			err = s.sendNegativeBalanceWebhook(ctx, sub, lastCreatedInvoice, walletBalanceAfter)
			if err != nil {
				s.Logger.Errorw("failed to send negative balance webhook",
					"error", err,
					"customer_id", sub.CustomerID)
				// Don't fail - this is just a notification
			}
		}
	}

	return nil
}

// GetOverageBillingConfig retrieves the overage billing configuration
func (s *overageBillingService) GetOverageBillingConfig(ctx context.Context) (*types.OverageBillingConfig, error) {
	// Get settings from repository by key
	setting, err := s.SettingsRepo.GetByKey(ctx, types.SettingKeyOverageBillingConfig)
	if err != nil {
		if ierr.IsNotFound(err) {
			// Return default config if not set
			return &types.OverageBillingConfig{
				Enabled:          true, // Default to enabled
				InvoiceThreshold: decimal.NewFromFloat(5.0),
			}, nil
		}
		return nil, err
	}

	// Convert setting value to OverageBillingConfig
	config, err := utils.ToStruct[types.OverageBillingConfig](setting.Value)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to parse overage billing config").
			Mark(ierr.ErrInvalidOperation)
	}

	return &config, nil
}

// hasOverageCost checks if the processed event has overage cost
// An event has overage cost if it has a positive cost
func (s *overageBillingService) hasOverageCost(event *events.ProcessedEvent) bool {
	return event.Cost.GreaterThan(decimal.Zero)
}

// getAccumulatedOverageCost gets the uninvoiced accumulated cost for a subscription period
// It calculates total period cost minus already invoiced overage amounts
func (s *overageBillingService) getAccumulatedOverageCost(
	ctx context.Context,
	event *events.ProcessedEvent,
	sub *dto.SubscriptionResponse,
) (decimal.Decimal, error) {
	// Get total period cost
	totalCost, err := s.processedEventRepo.GetPeriodCost(
		ctx,
		types.GetTenantID(ctx),
		types.GetEnvironmentID(ctx),
		sub.CustomerID,
		sub.ID,
		event.PeriodID,
	)
	if err != nil {
		return decimal.Zero, err
	}

	// Get already invoiced overage amount for this subscription/period
	alreadyInvoiced, err := s.getAlreadyInvoicedOverageAmount(ctx, sub.ID)
	if err != nil {
		s.Logger.Warnw("failed to get already invoiced amount, using total cost",
			"error", err,
			"subscription_id", sub.ID)
		// Fall back to total cost if we can't get invoiced amount
		return totalCost, nil
	}

	// Return uninvoiced amount
	uninvoicedAmount := totalCost.Sub(alreadyInvoiced)
	if uninvoicedAmount.LessThan(decimal.Zero) {
		// This shouldn't happen, but protect against it
		return decimal.Zero, nil
	}

	return uninvoicedAmount, nil
}

// getAlreadyInvoicedOverageAmount gets the sum of overage invoices for a subscription
func (s *overageBillingService) getAlreadyInvoicedOverageAmount(
	ctx context.Context,
	subscriptionID string,
) (decimal.Decimal, error) {
	// Query invoices for this subscription with billing_type=overage metadata
	filter := &types.InvoiceFilter{
		QueryFilter: &types.QueryFilter{
			Status: lo.ToPtr(types.StatusPublished),
		},
		SubscriptionID: subscriptionID,
		InvoiceType:    types.InvoiceTypeOneOff,
	}

	invoices, err := s.InvoiceRepo.List(ctx, filter)
	if err != nil {
		return decimal.Zero, err
	}

	// Sum up amounts from overage invoices
	totalInvoiced := decimal.Zero
	for _, inv := range invoices {
		// Check if this is an overage invoice by looking at metadata
		if inv.Metadata != nil {
			if billingType, ok := inv.Metadata["billing_type"]; ok && billingType == "overage" {
				totalInvoiced = totalInvoiced.Add(inv.AmountDue)
			}
		}
	}

	return totalInvoiced, nil
}

// getInvoicedBuckets returns a map of all bucket numbers that have been invoiced
// for a given subscription and period. The map key is the bucket number, value is true.
func (s *overageBillingService) getInvoicedBuckets(
	ctx context.Context,
	subscriptionID string,
	periodID uint64,
) (map[int64]bool, error) {
	// Query invoices for this subscription with billing_type=overage metadata
	// We limit to 1000 invoices per query - this should be more than enough for any period
	// since each invoice is $5 and 1000 invoices would mean $5000 of overage in a single period
	filter := &types.InvoiceFilter{
		QueryFilter: &types.QueryFilter{
			Status: lo.ToPtr(types.StatusPublished),
			Limit:  lo.ToPtr(1000),
		},
		SubscriptionID: subscriptionID,
		InvoiceType:    types.InvoiceTypeOneOff,
	}

	invoices, err := s.InvoiceRepo.List(ctx, filter)
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

		// Check billing_type first
		billingType, ok := inv.Metadata["billing_type"]
		if !ok || billingType != "overage" {
			continue
		}

		// Try to match by period_id metadata first (faster)
		if storedPeriodID, ok := inv.Metadata["period_id"]; ok {
			if storedPeriodID != periodIDStr {
				continue // Different period, skip
			}
		}

		// Extract bucket number from idempotency key
		// Format: overage_{subID}_{periodID}_{bucket}
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

// createOverageInvoice creates an invoice for overage charges
func (s *overageBillingService) createOverageInvoice(
	ctx context.Context,
	sub *dto.SubscriptionResponse,
	amount decimal.Decimal,
	idempotencyKey string,
	periodID uint64,
) (*invoice.Invoice, error) {
	invoiceService := NewInvoiceService(s.ServiceParams)

	// Create a simple one-off invoice for overage
	now := time.Now().UTC()
	invoiceReq := dto.CreateInvoiceRequest{
		IdempotencyKey: lo.ToPtr(idempotencyKey), // Prevents duplicate invoices
		CustomerID:     sub.CustomerID,
		SubscriptionID: lo.ToPtr(sub.ID),
		InvoiceType:    types.InvoiceTypeOneOff,
		BillingReason:  types.InvoiceBillingReasonManual,
		Currency:       sub.Currency,
		AmountDue:      amount,
		AmountPaid:     lo.ToPtr(decimal.Zero),                       // Explicitly set as unpaid
		PaymentStatus:  lo.ToPtr(types.PaymentStatusPending),         // Pending payment until wallet deduction
		Total:          amount,
		Subtotal:       amount,
		PeriodStart:    lo.ToPtr(sub.CurrentPeriodStart),
		PeriodEnd:      lo.ToPtr(now),
		Description:    "Real-time overage charges",
		LineItems: []dto.CreateInvoiceLineItemRequest{
			{
				DisplayName: lo.ToPtr("Overage Charges"),
				Amount:      amount,
				Quantity:    decimal.NewFromInt(1),
				PriceType:   lo.ToPtr(string(types.PRICE_TYPE_USAGE)),
				PeriodStart: lo.ToPtr(sub.CurrentPeriodStart),
				PeriodEnd:   lo.ToPtr(now),
				Metadata: types.Metadata{
					"billing_type":      "overage",
					"subscription_id":   sub.ID,
					"idempotency_key":   idempotencyKey,
				},
			},
		},
		Metadata: types.Metadata{
			"billing_type":    "overage",
			"subscription_id": sub.ID,
			"idempotency_key": idempotencyKey,
			"period_id":       fmt.Sprintf("%d", periodID),
		},
	}

	resp, err := invoiceService.CreateInvoice(ctx, invoiceReq)
	if err != nil {
		return nil, err
	}

	// Finalize the invoice immediately
	err = invoiceService.FinalizeInvoice(ctx, resp.ID)
	if err != nil {
		s.Logger.Warnw("failed to finalize overage invoice, continuing with draft",
			"error", err,
			"invoice_id", resp.ID)
	}

	// Fetch the invoice domain object
	inv, err := s.InvoiceRepo.Get(ctx, resp.ID)
	if err != nil {
		return nil, err
	}

	return inv, nil
}

// processWalletPayment processes wallet payment for an invoice
// Returns the amount paid
func (s *overageBillingService) processWalletPayment(
	ctx context.Context,
	inv *invoice.Invoice,
) (decimal.Decimal, error) {
	walletPaymentService := NewWalletPaymentService(s.ServiceParams)

	options := DefaultWalletPaymentOptions()
	options.AllowNegativeBalance = true // Allow wallet to go negative for overage billing
	options.AdditionalMetadata = types.Metadata{
		"billing_type": "overage",
		"invoice_id":   inv.ID,
	}

	return walletPaymentService.ProcessInvoicePaymentWithWallets(ctx, inv, options)
}

// getCustomerWalletBalance gets the total wallet balance for a customer
func (s *overageBillingService) getCustomerWalletBalance(
	ctx context.Context,
	customerID string,
) (decimal.Decimal, error) {
	wallets, err := s.WalletRepo.GetWalletsByCustomerID(ctx, customerID)
	if err != nil {
		return decimal.Zero, err
	}

	totalBalance := decimal.Zero
	for _, w := range wallets {
		if w.WalletStatus == types.WalletStatusActive {
			totalBalance = totalBalance.Add(w.Balance)
		}
	}

	return totalBalance, nil
}

// sendNegativeBalanceWebhook sends a webhook notification when wallet crosses to negative
func (s *overageBillingService) sendNegativeBalanceWebhook(
	ctx context.Context,
	sub *dto.SubscriptionResponse,
	inv *invoice.Invoice,
	walletBalance decimal.Decimal,
) error {
	// Get the primary wallet for this customer to include in the webhook
	wallets, err := s.WalletRepo.GetWalletsByCustomerID(ctx, sub.CustomerID)
	if err != nil {
		return err
	}

	var walletID string
	if len(wallets) > 0 {
		walletID = wallets[0].ID
	}

	// Create the webhook payload
	payload := webhookDto.WalletNegativeBalancePayload{
		CustomerID:     sub.CustomerID,
		SubscriptionID: sub.ID,
		WalletID:       walletID,
		WalletBalance:  walletBalance,
		Currency:       sub.Currency,
		InvoiceID:      inv.ID,
		Timestamp:      time.Now().UTC(),
	}

	webhookPayload, err := json.Marshal(payload)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to marshal webhook payload").
			Mark(ierr.ErrInvalidOperation)
	}

	webhookEvent := &types.WebhookEvent{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WEBHOOK_EVENT),
		EventName:     types.WebhookEventWalletBalanceNegative,
		TenantID:      types.GetTenantID(ctx),
		EnvironmentID: types.GetEnvironmentID(ctx),
		UserID:        types.GetUserID(ctx),
		Timestamp:     time.Now().UTC(),
		Payload:       json.RawMessage(webhookPayload),
	}

	if err := s.WebhookPublisher.PublishWebhook(ctx, webhookEvent); err != nil {
		s.Logger.Errorw("failed to publish wallet negative balance webhook",
			"error", err,
			"customer_id", sub.CustomerID,
			"subscription_id", sub.ID)
		return err
	}

	s.Logger.Infow("published wallet negative balance webhook",
		"customer_id", sub.CustomerID,
		"subscription_id", sub.ID,
		"wallet_balance", walletBalance.String(),
		"invoice_id", inv.ID)

	return nil
}
