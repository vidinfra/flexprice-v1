package sslcommerz

import (
	"context"
	"strings"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// PaymentService handles SSLCommerz payment operations
type PaymentService struct {
	client SSLCommerzClient
	logger *logger.Logger
}

// NewPaymentService creates a new SSLCommerz payment service
func NewPaymentService(
	client SSLCommerzClient,
	logger *logger.Logger,
) *PaymentService {
	return &PaymentService{
		client: client,
		logger: logger,
	}
}

// CreatePaymentLink creates an SSLCommerz payment link
func (s *PaymentService) CreatePaymentLink(
	ctx context.Context,
	req *CreatePaymentLinkRequest,
	customerService interfaces.CustomerService,
	invoiceService interfaces.InvoiceService,
) (*SSLCommerzPaymentLinkResponse, error) {
	s.logger.Infow("creating SSLCommerz payment link",
		"invoice_id", req.InvoiceID,
		"customer_id", req.CustomerID,
		"amount", req.Amount.String(),
		"currency", req.Currency,
		"environment_id", req.EnvironmentID,
	)

	// Validate invoice and check payment eligibility
	invoiceResp, err := invoiceService.GetInvoice(ctx, req.InvoiceID)
	if err != nil {
		s.logger.Errorw("failed to get invoice",
			"invoice_id", req.InvoiceID,
			"error", err)
		return nil, ierr.WithError(err).
			WithHint("Failed to get invoice").
			WithReportableDetails(map[string]interface{}{
				"invoice_id": req.InvoiceID,
			}).
			Mark(ierr.ErrNotFound)
	}

	// Validate invoice payment status
	if invoiceResp.PaymentStatus == types.PaymentStatusSucceeded {
		return nil, ierr.NewError("invoice is already paid").
			WithHint("Cannot create payment link for an already paid invoice").
			WithReportableDetails(map[string]interface{}{
				"invoice_id":     req.InvoiceID,
				"payment_status": invoiceResp.PaymentStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	if invoiceResp.InvoiceStatus == types.InvoiceStatusVoided {
		return nil, ierr.NewError("invoice is voided").
			WithHint("Cannot create payment link for a voided invoice").
			WithReportableDetails(map[string]interface{}{
				"invoice_id":     req.InvoiceID,
				"invoice_status": invoiceResp.InvoiceStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	// Validate payment amount against invoice remaining balance
	if req.Amount.GreaterThan(invoiceResp.AmountRemaining) {
		return nil, ierr.NewError("payment amount exceeds invoice remaining balance").
			WithHint("Payment amount cannot be greater than the remaining balance on the invoice").
			WithReportableDetails(map[string]interface{}{
				"invoice_id":        req.InvoiceID,
				"payment_amount":    req.Amount.String(),
				"invoice_remaining": invoiceResp.AmountRemaining.String(),
				"invoice_total":     invoiceResp.AmountDue.String(),
				"invoice_paid":      invoiceResp.AmountPaid.String(),
			}).
			Mark(ierr.ErrValidation)
	}

	// Validate currency matches invoice currency
	if req.Currency != invoiceResp.Currency {
		return nil, ierr.NewError("payment currency does not match invoice currency").
			WithHint("Payment currency must match the invoice currency").
			WithReportableDetails(map[string]interface{}{
				"invoice_id":       req.InvoiceID,
				"payment_currency": req.Currency,
				"invoice_currency": invoiceResp.Currency,
			}).
			Mark(ierr.ErrValidation)
	}

	// Get SSLCommerz configuration
	sslConfig, err := s.client.GetSSLCommerzConfig(ctx)
	if err != nil {
		return nil, err
	}

	// Get connection for tenant/environment IDs
	conn, err := s.client.GetConnection(ctx)
	if err != nil {
		return nil, err
	}

	// Set default URLs if not provided
	successURL := req.SuccessURL
	if successURL == "" {
		successURL = sslConfig.SuccessURL
	}
	if successURL == "" {
		successURL = "http://localhost:8080/billing/payment/success"
	}

	failURL := req.FailURL
	if failURL == "" {
		failURL = sslConfig.FailURL
	}
	if failURL == "" {
		failURL = successURL
	}

	cancelURL := req.CancelURL
	if cancelURL == "" {
		cancelURL = sslConfig.CancelURL
	}
	if cancelURL == "" {
		cancelURL = successURL
	}

	// Get customer details
	customer, err := customerService.GetCustomer(ctx, req.CustomerID)
	if err != nil {
		s.logger.Warnw("failed to get customer details, using request data",
			"customer_id", req.CustomerID,
			"error", err)
	}

	// Build customer details with fallbacks
	customerName := req.CustomerName
	customerEmail := req.CustomerEmail
	customerPhone := req.CustomerPhone
	customerAddress := req.CustomerAddress
	customerCity := req.CustomerCity
	customerPostcode := req.CustomerPostcode
	customerCountry := req.CustomerCountry

	if customer != nil {
		if customerName == "" {
			customerName = customer.Name
		}
		if customerEmail == "" {
			customerEmail = customer.Email
		}
		// Use customer ID as fallback for phone if not provided
		if customerPhone == "" {
			customerPhone = req.CustomerID
		}
	}

	// Set defaults for required fields
	if customerName == "" {
		customerName = "Customer"
	}
	if customerEmail == "" {
		customerEmail = req.CustomerID
	}
	if customerPhone == "" {
		customerPhone = req.CustomerID
	}
	if customerAddress == "" {
		customerAddress = "N/A"
	}
	if customerCity == "" {
		customerCity = "N/A"
	}
	if customerPostcode == "" {
		customerPostcode = "0000"
	}
	if customerCountry == "" {
		customerCountry = "Bangladesh"
	}

	// Build form data for SSLCommerz API
	formData := &SSLCommerzFormData{
		TotalAmount:     req.Amount.String(),
		Currency:        strings.ToUpper(req.Currency),
		TranID:          req.InvoiceID, // Use invoice ID as transaction ID
		SuccessURL:      successURL,
		FailURL:         failURL,
		CancelURL:       cancelURL,
		IPNURL:          sslConfig.IPNURL,
		CusName:         customerName,
		CusEmail:        customerEmail,
		CusAdd1:         customerAddress,
		CusCity:         customerCity,
		CusPostcode:     customerPostcode,
		CusCountry:      customerCountry,
		CusPhone:        customerPhone,
		ShippingMethod:  DefaultShippingMethod,
		ProductName:     DefaultProductName,
		ProductCategory: DefaultProductCategory,
		ProductProfile:  DefaultProductProfile,
		ValueA:          conn.TenantID,      // Tenant ID for webhook context
		ValueB:          conn.EnvironmentID, // Environment ID for webhook context
		ValueC:          req.PaymentID,      // FlexPrice payment ID
		ValueD:          "",                 // Additional metadata if needed
	}

	s.logger.Infow("creating SSLCommerz payment session",
		"invoice_id", req.InvoiceID,
		"customer_id", req.CustomerID,
		"amount", req.Amount.String(),
		"currency", req.Currency,
		"tran_id", formData.TranID)

	// Create payment session with SSLCommerz
	apiResponse, err := s.client.CreatePaymentSession(ctx, formData)
	if err != nil {
		return nil, err
	}

	// Validate response
	if apiResponse.GatewayPageURL == "" {
		return nil, ierr.NewError("failed to create payment link").
			WithHint("SSLCommerz API did not return a payment URL").
			WithReportableDetails(map[string]interface{}{
				"invoice_id": req.InvoiceID,
				"response":   apiResponse,
			}).
			Mark(ierr.ErrInternal)
	}

	response := &SSLCommerzPaymentLinkResponse{
		ID:                 apiResponse.SessionKey,
		PaymentURL:         apiResponse.GatewayPageURL,
		Amount:             req.Amount,
		Currency:           req.Currency,
		Status:             apiResponse.Status,
		CreatedAt:          time.Now().Unix(),
		PaymentID:          req.PaymentID,
		SessionKey:         apiResponse.SessionKey,
		GatewayPageURL:     apiResponse.GatewayPageURL,
		RedirectGatewayURL: apiResponse.RedirectGatewayURL,
		StoreBanner:        apiResponse.StoreBanner,
		StoreLogo:          apiResponse.StoreLogo,
		FailedReason:       apiResponse.FailedReason,
	}

	s.logger.Infow("successfully created SSLCommerz payment link",
		"payment_id", response.PaymentID,
		"session_key", apiResponse.SessionKey,
		"payment_url", apiResponse.GatewayPageURL,
		"invoice_id", req.InvoiceID,
		"amount", req.Amount.String(),
		"currency", req.Currency,
	)

	return response, nil
}

// ReconcilePaymentWithInvoice updates the invoice payment status and amounts when a payment succeeds
func (s *PaymentService) ReconcilePaymentWithInvoice(
	ctx context.Context,
	paymentID string,
	paymentAmount decimal.Decimal,
	paymentService interfaces.PaymentService,
	invoiceService interfaces.InvoiceService,
) error {
	s.logger.Infow("starting payment reconciliation with invoice",
		"payment_id", paymentID,
		"payment_amount", paymentAmount.String())

	// Get the payment record
	payment, err := paymentService.GetPayment(ctx, paymentID)
	if err != nil {
		s.logger.Errorw("failed to get payment record for reconciliation",
			"error", err,
			"payment_id", paymentID)
		return err
	}

	// Reconcile the invoice
	return s.reconcileInvoice(ctx, payment.DestinationID, paymentAmount, invoiceService)
}

// reconcileInvoice is the shared logic for invoice reconciliation
func (s *PaymentService) reconcileInvoice(
	ctx context.Context,
	invoiceID string,
	paymentAmount decimal.Decimal,
	invoiceService interfaces.InvoiceService,
) error {
	// Get the invoice
	invoiceResp, err := invoiceService.GetInvoice(ctx, invoiceID)
	if err != nil {
		s.logger.Errorw("failed to get invoice for reconciliation",
			"error", err,
			"invoice_id", invoiceID)
		return err
	}

	// Calculate new amounts
	newAmountPaid := invoiceResp.AmountPaid.Add(paymentAmount)
	newAmountRemaining := invoiceResp.AmountDue.Sub(newAmountPaid)

	// Determine payment status
	var newPaymentStatus types.PaymentStatus
	if newAmountRemaining.IsZero() {
		newPaymentStatus = types.PaymentStatusSucceeded
	} else if newAmountRemaining.IsNegative() {
		newPaymentStatus = types.PaymentStatusOverpaid
		newAmountRemaining = decimal.Zero
	} else {
		newPaymentStatus = types.PaymentStatusPending
	}

	s.logger.Infow("calculated new amounts for reconciliation",
		"invoice_id", invoiceID,
		"payment_amount", paymentAmount.String(),
		"new_amount_paid", newAmountPaid.String(),
		"new_amount_remaining", newAmountRemaining.String(),
		"new_payment_status", newPaymentStatus)

	// Update invoice
	err = invoiceService.ReconcilePaymentStatus(ctx, invoiceID, newPaymentStatus, &paymentAmount)
	if err != nil {
		s.logger.Errorw("failed to update invoice payment status",
			"error", err,
			"invoice_id", invoiceID)
		return err
	}

	s.logger.Infow("successfully reconciled invoice",
		"invoice_id", invoiceID,
		"payment_amount", paymentAmount.String(),
		"new_payment_status", newPaymentStatus)

	return nil
}
