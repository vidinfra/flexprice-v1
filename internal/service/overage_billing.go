package service

import (
	"context"
	"encoding/json"
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

	// 3. Get accumulated overage cost for this subscription period
	accumulatedCost, err := s.getAccumulatedOverageCost(ctx, processedEvent, sub)
	if err != nil {
		s.Logger.Errorw("failed to get accumulated overage cost",
			"error", err,
			"subscription_id", sub.ID,
			"period_id", processedEvent.PeriodID)
		return nil // Don't fail event processing
	}

	s.Logger.Debugw("accumulated overage cost",
		"subscription_id", sub.ID,
		"period_id", processedEvent.PeriodID,
		"accumulated_cost", accumulatedCost.String(),
		"threshold", config.InvoiceThreshold.String())

	// 4. Check if accumulated cost meets threshold
	if accumulatedCost.LessThan(config.InvoiceThreshold) {
		return nil // Not yet at threshold
	}

	s.Logger.Infow("overage threshold reached, creating invoice",
		"subscription_id", sub.ID,
		"accumulated_cost", accumulatedCost.String(),
		"threshold", config.InvoiceThreshold.String())

	// 5. Create overage invoice
	inv, err := s.createOverageInvoice(ctx, sub, accumulatedCost)
	if err != nil {
		s.Logger.Errorw("failed to create overage invoice",
			"error", err,
			"subscription_id", sub.ID,
			"accumulated_cost", accumulatedCost.String())
		return nil // Don't fail event processing
	}

	s.Logger.Infow("created overage invoice",
		"invoice_id", inv.ID,
		"subscription_id", sub.ID,
		"amount", inv.AmountDue.String())

	// 6. Get wallet balance before payment
	walletBalanceBefore, err := s.getCustomerWalletBalance(ctx, sub.CustomerID)
	if err != nil {
		s.Logger.Errorw("failed to get wallet balance before payment",
			"error", err,
			"customer_id", sub.CustomerID)
		// Continue with payment attempt
		walletBalanceBefore = decimal.Zero
	}

	// 7. Process wallet payment (allow negative balance)
	_, err = s.processWalletPayment(ctx, inv)
	if err != nil {
		s.Logger.Errorw("failed to process wallet payment for overage invoice",
			"error", err,
			"invoice_id", inv.ID,
			"customer_id", sub.CustomerID)
		// Don't fail - invoice was created
	}

	// 8. Get wallet balance after payment
	walletBalanceAfter, err := s.getCustomerWalletBalance(ctx, sub.CustomerID)
	if err != nil {
		s.Logger.Errorw("failed to get wallet balance after payment",
			"error", err,
			"customer_id", sub.CustomerID)
		return nil
	}

	// 9. Check if wallet crossed from positive to negative
	if walletBalanceBefore.GreaterThanOrEqual(decimal.Zero) && walletBalanceAfter.LessThan(decimal.Zero) {
		s.Logger.Infow("wallet crossed to negative balance, sending webhook",
			"customer_id", sub.CustomerID,
			"subscription_id", sub.ID,
			"balance_before", walletBalanceBefore.String(),
			"balance_after", walletBalanceAfter.String())

		err = s.sendNegativeBalanceWebhook(ctx, sub, inv, walletBalanceAfter)
		if err != nil {
			s.Logger.Errorw("failed to send negative balance webhook",
				"error", err,
				"customer_id", sub.CustomerID)
			// Don't fail - this is just a notification
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

// getAccumulatedOverageCost gets the accumulated cost for a subscription period
func (s *overageBillingService) getAccumulatedOverageCost(
	ctx context.Context,
	event *events.ProcessedEvent,
	sub *dto.SubscriptionResponse,
) (decimal.Decimal, error) {
	return s.processedEventRepo.GetPeriodCost(
		ctx,
		types.GetTenantID(ctx),
		types.GetEnvironmentID(ctx),
		sub.CustomerID,
		sub.ID,
		event.PeriodID,
	)
}

// createOverageInvoice creates an invoice for overage charges
func (s *overageBillingService) createOverageInvoice(
	ctx context.Context,
	sub *dto.SubscriptionResponse,
	amount decimal.Decimal,
) (*invoice.Invoice, error) {
	invoiceService := NewInvoiceService(s.ServiceParams)

	// Create a simple one-off invoice for overage
	now := time.Now().UTC()
	invoiceReq := dto.CreateInvoiceRequest{
		CustomerID:     sub.CustomerID,
		SubscriptionID: lo.ToPtr(sub.ID),
		InvoiceType:    types.InvoiceTypeOneOff,
		BillingReason:  types.InvoiceBillingReasonManual,
		Currency:       sub.Currency,
		AmountDue:      amount,
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
					"billing_type":    "overage",
					"subscription_id": sub.ID,
				},
			},
		},
		Metadata: types.Metadata{
			"billing_type":    "overage",
			"subscription_id": sub.ID,
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
