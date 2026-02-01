package webhook

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/integration/sslcommerz"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// Handler handles SSLCommerz webhook events
type Handler struct {
	client     sslcommerz.SSLCommerzClient
	paymentSvc *sslcommerz.PaymentService
	logger     *logger.Logger
}

// NewHandler creates a new SSLCommerz webhook handler
func NewHandler(
	client sslcommerz.SSLCommerzClient,
	paymentSvc *sslcommerz.PaymentService,
	logger *logger.Logger,
) *Handler {
	return &Handler{
		client:     client,
		paymentSvc: paymentSvc,
		logger:     logger,
	}
}

// ServiceDependencies contains all service dependencies needed by webhook handlers
type ServiceDependencies = interfaces.ServiceDependencies

// HandleIPN processes the SSLCommerz IPN (Instant Payment Notification) webhook
// This function never returns errors to ensure webhooks always return 200 OK
// All errors are logged internally to prevent SSLCommerz from retrying
func (h *Handler) HandleIPN(
	ctx context.Context,
	ipnData *SSLCommerzIPNData,
	services *ServiceDependencies,
) error {
	h.logger.Infow("processing SSLCommerz IPN webhook",
		"tran_id", ipnData.TranID,
		"val_id", ipnData.ValID,
		"status", ipnData.Status,
		"amount", ipnData.Amount,
		"currency", ipnData.Currency,
		"risk_level", ipnData.RiskLevel,
		"tenant_id", ipnData.ValueA,
		"environment_id", ipnData.ValueB,
		"payment_id", ipnData.ValueC,
	)

	// Validate required fields
	if ipnData.TranID == "" || ipnData.Status == "" || ipnData.ValID == "" {
		h.logger.Errorw("missing required SSLCommerz IPN fields",
			"tran_id", ipnData.TranID,
			"status", ipnData.Status,
			"val_id", ipnData.ValID)
		return nil // Don't fail webhook processing
	}

	// Validate with SSLCommerz Order Validation API
	validationResp, err := h.client.ValidatePaymentOrder(ctx, ipnData.ValID)
	if err != nil {
		h.logger.Errorw("failed to validate SSLCommerz payment order",
			"error", err,
			"tran_id", ipnData.TranID,
			"val_id", ipnData.ValID)
		return nil // Don't fail webhook processing
	}

	h.logger.Infow("SSLCommerz order validation response",
		"tran_id", ipnData.TranID,
		"val_id", ipnData.ValID,
		"validation_status", validationResp.Status,
		"risk_level", validationResp.RiskLevel)

	// Check for high-risk transactions
	if ipnData.RiskLevel == "1" && ipnData.Status == "VALID" {
		h.logger.Warnw("SSLCommerz payment is VALID but risk_level=1, holding for manual verification",
			"tran_id", ipnData.TranID,
			"val_id", ipnData.ValID,
			"risk_level", ipnData.RiskLevel)

		err = h.updatePaymentStatus(ctx, ipnData, "PENDING_VERIFICATION",
			lo.ToPtr("Payment is valid but risk level is high; manual verification required."),
			services)
		if err != nil {
			h.logger.Errorw("failed to update payment status for high-risk transaction",
				"error", err,
				"tran_id", ipnData.TranID)
		}
		return nil
	}

	// Determine final status from validation response
	updateStatus := validationResp.Status
	var errorMsg *string
	if updateStatus != "VALID" && updateStatus != "VALIDATED" {
		errMsg := "Payment validation failed with status: " + updateStatus
		errorMsg = &errMsg
	}

	// Update payment status
	err = h.updatePaymentStatus(ctx, ipnData, updateStatus, errorMsg, services)
	if err != nil {
		h.logger.Errorw("failed to update payment status from SSLCommerz IPN",
			"error", err,
			"tran_id", ipnData.TranID,
			"val_id", ipnData.ValID,
			"status", updateStatus)
		return nil // Don't fail webhook processing
	}

	// If payment succeeded, reconcile invoice and top up wallet
	if updateStatus == "VALID" || updateStatus == "VALIDATED" {
		err = h.handleSuccessfulPayment(ctx, ipnData, services)
		if err != nil {
			h.logger.Errorw("failed to handle successful payment",
				"error", err,
				"tran_id", ipnData.TranID)
			// Don't fail - payment status was already updated
		}
	}

	h.logger.Infow("successfully processed SSLCommerz IPN",
		"tran_id", ipnData.TranID,
		"val_id", ipnData.ValID,
		"status", updateStatus)

	return nil
}

// updatePaymentStatus updates the payment status in the database
func (h *Handler) updatePaymentStatus(
	ctx context.Context,
	ipnData *SSLCommerzIPNData,
	status string,
	errorMsg *string,
	services *ServiceDependencies,
) error {
	// Find payment by PaymentID (tran_id now contains PaymentID)
	payments, err := services.PaymentService.ListPayments(ctx, &types.PaymentFilter{
		PaymentIDs:      []string{ipnData.TranID},
		DestinationType: lo.ToPtr(string(types.PaymentDestinationTypeInvoice)),
		QueryFilter:     types.NewNoLimitQueryFilter(),
	})
	if err != nil {
		h.logger.Errorw("failed to fetch payment for SSLCommerz",
			"error", err,
			"payment_id", ipnData.TranID)
		return err
	}

	if len(payments.Items) == 0 {
		h.logger.Warnw("no payment record found for SSLCommerz transaction",
			"payment_id", ipnData.TranID)
		return nil // Not an error - payment might not exist yet
	}

	payment := payments.Items[0]

	// Check if payment is already processed
	if payment.PaymentStatus == types.PaymentStatusSucceeded {
		h.logger.Infow("payment already processed",
			"payment_id", payment.ID,
			"tran_id", ipnData.TranID,
			"status", payment.PaymentStatus)
		return nil
	}

	var succeededAt *time.Time
	var failedAt *time.Time
	var paymentStatus string

	switch SSLCommerzPaymentStatus(status) {
	case StatusValid, StatusValidated:
		paymentStatus = string(types.PaymentStatusSucceeded)
		now := time.Now()
		succeededAt = &now
	case StatusFailed, StatusCancelled, StatusExpired:
		paymentStatus = string(types.PaymentStatusFailed)
		now := time.Now()
		failedAt = &now
	default:
		paymentStatus = string(types.PaymentStatusPending)
	}

	updateReq := dto.UpdatePaymentRequest{
		PaymentStatus:    &paymentStatus,
		GatewayPaymentID: &ipnData.TranID,
	}

	if succeededAt != nil {
		updateReq.SucceededAt = succeededAt
	}
	if failedAt != nil {
		updateReq.FailedAt = failedAt
	}
	if errorMsg != nil {
		updateReq.ErrorMessage = errorMsg
	}

	// Add card details to metadata if available
	if ipnData.CardType != "" || ipnData.CardBrand != "" {
		h.logger.Infow("payment method details from SSLCommerz",
			"payment_id", payment.ID,
			"card_type", ipnData.CardType,
			"card_brand", ipnData.CardBrand,
			"card_issuer", ipnData.CardIssuer)
	}

	_, err = services.PaymentService.UpdatePayment(ctx, payment.ID, updateReq)
	if err != nil {
		h.logger.Errorw("failed to update SSLCommerz payment status",
			"error", err,
			"payment_id", payment.ID)
		return err
	}

	h.logger.Infow("successfully updated SSLCommerz payment status",
		"payment_id", payment.ID,
		"tran_id", ipnData.TranID,
		"status", paymentStatus)

	return nil
}

// handleSuccessfulPayment handles post-payment success actions (reconcile invoice, top up wallet)
func (h *Handler) handleSuccessfulPayment(
	ctx context.Context,
	ipnData *SSLCommerzIPNData,
	services *ServiceDependencies,
) error {
	// Find payment by PaymentID (tran_id now contains PaymentID)
	payments, err := services.PaymentService.ListPayments(ctx, &types.PaymentFilter{
		PaymentIDs:      []string{ipnData.TranID},
		DestinationType: lo.ToPtr(string(types.PaymentDestinationTypeInvoice)),
		QueryFilter:     types.NewNoLimitQueryFilter(),
	})
	if err != nil {
		h.logger.Errorw("failed to fetch payment for reconciliation/topup after SSLCommerz success",
			"error", err,
			"payment_id", ipnData.TranID)
		return err
	}

	if len(payments.Items) == 0 {
		h.logger.Warnw("no payment found for reconciliation after SSLCommerz success",
			"payment_id", ipnData.TranID)
		return nil
	}

	payment := payments.Items[0]

	// IMPORTANT: Use the original payment amount (in original currency, e.g., USD)
	// NOT the IPN amount which is in the local currency (e.g., BDT)
	// The payment record stores the amount in the invoice's currency
	amount := payment.Amount

	h.logger.Infow("reconciling invoice with original payment amount",
		"payment_id", payment.ID,
		"invoice_id", payment.DestinationID,
		"original_amount", amount.String(),
		"original_currency", payment.Currency,
		"ipn_amount", ipnData.Amount,
		"ipn_currency", ipnData.Currency)

	// Reconcile invoice with the original payment amount (USD), not the IPN amount (BDT)
	// NOTE: ReconcilePaymentStatus already handles wallet credit for wallet top-up invoices
	// by checking for wallet_transaction_id in invoice metadata and calling
	// CompletePurchasedCreditTransactionWithRetry
	err = services.InvoiceService.ReconcilePaymentStatus(ctx, payment.DestinationID, types.PaymentStatusSucceeded, &amount)
	if err != nil {
		h.logger.Errorw("failed to reconcile invoice after SSLCommerz payment success",
			"error", err,
			"payment_id", payment.ID,
			"invoice_id", payment.DestinationID)
		// Don't return error - payment status was already updated
	} else {
		h.logger.Infow("successfully reconciled invoice after SSLCommerz payment",
			"payment_id", payment.ID,
			"invoice_id", payment.DestinationID,
			"amount", amount.String(),
			"currency", payment.Currency)
	}

	return nil
}

// HandleSuccessRedirect handles the customer redirect after successful payment
func (h *Handler) HandleSuccessRedirect(
	ctx context.Context,
	data *SSLCommerzRedirectData,
) error {
	h.logger.Infow("SSLCommerz success redirect callback",
		"tran_id", data.TranID,
		"val_id", data.ValID,
		"status", data.Status,
		"amount", data.Amount,
		"currency", data.Currency)

	// The actual payment processing is done via IPN
	// This is just for customer redirect logging
	return nil
}

// HandleFailRedirect handles the customer redirect after failed payment
func (h *Handler) HandleFailRedirect(
	ctx context.Context,
	data *SSLCommerzRedirectData,
) error {
	h.logger.Infow("SSLCommerz fail redirect callback",
		"tran_id", data.TranID,
		"val_id", data.ValID,
		"status", data.Status)

	// The actual payment processing is done via IPN
	// This is just for customer redirect logging
	return nil
}

// HandleCancelRedirect handles the customer redirect after cancelled payment
func (h *Handler) HandleCancelRedirect(
	ctx context.Context,
	data *SSLCommerzRedirectData,
) error {
	h.logger.Infow("SSLCommerz cancel redirect callback",
		"tran_id", data.TranID,
		"val_id", data.ValID,
		"status", data.Status)

	// The actual payment processing is done via IPN
	// This is just for customer redirect logging
	return nil
}
