package sslcommerz

import (
	"github.com/shopspring/decimal"
)

// Constants for SSLCommerz integration
const (
	// DefaultProductName is the default product name for payments
	DefaultProductName = "Service Payment"

	// DefaultProductCategory is the default product category
	DefaultProductCategory = "Service"

	// DefaultProductProfile is the default product profile
	DefaultProductProfile = "general"

	// DefaultShippingMethod is the default shipping method (NO for service payments)
	DefaultShippingMethod = "NO"
)

// SSLCommerzConfig holds decrypted SSLCommerz configuration
type SSLCommerzConfig struct {
	StoreID       string
	StorePassword string
	BaseURL       string // API base URL (sandbox or production)
	SessionAPI    string // Session API endpoint for creating transactions
	ValidationAPI string // Validation API endpoint for verifying payments
	IPNURL        string // IPN webhook URL
	SuccessURL    string // Default success redirect URL
	FailURL       string // Default fail redirect URL
	CancelURL     string // Default cancel redirect URL
}

// CreatePaymentLinkRequest represents FlexPrice request to create an SSLCommerz payment link
type CreatePaymentLinkRequest struct {
	InvoiceID     string
	CustomerID    string
	Amount        decimal.Decimal
	Currency      string
	SuccessURL    string
	FailURL       string
	CancelURL     string
	Metadata      map[string]string
	PaymentID     string
	EnvironmentID string

	// Customer details
	CustomerName     string
	CustomerEmail    string
	CustomerPhone    string
	CustomerAddress  string
	CustomerCity     string
	CustomerPostcode string
	CustomerCountry  string
}

// SSLCommerzPaymentLinkResponse represents the response after creating a payment link
type SSLCommerzPaymentLinkResponse struct {
	ID                 string          // Session key
	PaymentURL         string          // Gateway page URL for redirect
	Amount             decimal.Decimal // Amount in original currency
	Currency           string          // Currency code
	Status             string          // Response status
	CreatedAt          int64           // Unix timestamp
	PaymentID          string          // FlexPrice payment ID
	SessionKey         string          // SSLCommerz session key
	RedirectGatewayURL string          // Redirect gateway URL
	StoreBanner        string          // Store banner URL
	StoreLogo          string          // Store logo URL
	FailedReason       string          // Reason for failure if any
}

// SSLCommerzAPIResponse represents the raw API response from SSLCommerz
type SSLCommerzAPIResponse struct {
	Status             string `json:"status"`
	FailedReason       string `json:"failedreason"`
	SessionKey         string `json:"sessionkey"`
	GatewayPageURL     string `json:"GatewayPageURL"`
	RedirectGatewayURL string `json:"redirectGatewayURL"`
	DirectPaymentURL   string `json:"directPaymentURL"`
	StoreBanner        string `json:"storeBanner"`
	StoreLogo          string `json:"storeLogo"`
	Desc               []struct {
		Name string `json:"name"`
		Type string `json:"type"`
		Logo string `json:"logo"`
		Gw   string `json:"gw"`
	} `json:"desc"`
}

// SSLCommerzValidationResponse represents the order validation API response
type SSLCommerzValidationResponse struct {
	Status                string `json:"status"`
	TranDate              string `json:"tran_date"`
	TranID                string `json:"tran_id"`
	ValID                 string `json:"val_id"`
	Amount                string `json:"amount"`
	StoreAmount           string `json:"store_amount"`
	Currency              string `json:"currency"`
	BankTranID            string `json:"bank_tran_id"`
	CardType              string `json:"card_type"`
	CardNo                string `json:"card_no"`
	CardIssuer            string `json:"card_issuer"`
	CardBrand             string `json:"card_brand"`
	CardIssuerCountry     string `json:"card_issuer_country"`
	CardIssuerCountryCode string `json:"card_issuer_country_code"`
	CurrencyType          string `json:"currency_type"`
	CurrencyAmount        string `json:"currency_amount"`
	CurrencyRate          string `json:"currency_rate"`
	BaseFare              string `json:"base_fair"` // SSLCommerz API uses "base_fair"
	ValueA                string `json:"value_a"`
	ValueB                string `json:"value_b"`
	ValueC                string `json:"value_c"`
	ValueD                string `json:"value_d"`
	EmiInstalment         string `json:"emi_instalment"`
	EmiAmount             string `json:"emi_amount"`
	EmiDescription        string `json:"emi_description"`
	EmiIssuer             string `json:"emi_issuer"`
	AccountDetails        string `json:"account_details"`
	RiskTitle             string `json:"risk_title"`
	RiskLevel             string `json:"risk_level"`
	APIConnect            string `json:"APIConnect"`
	ValidatedOn           string `json:"validated_on"`
	GwVersion             string `json:"gw_version"`
}

// SSLCommerzFormData represents the form data to send to SSLCommerz API
type SSLCommerzFormData struct {
	StoreID         string
	StorePassword   string
	TotalAmount     string
	Currency        string
	TranID          string
	SuccessURL      string
	FailURL         string
	CancelURL       string
	IPNURL          string
	CusName         string
	CusEmail        string
	CusAdd1         string
	CusCity         string
	CusPostcode     string
	CusCountry      string
	CusPhone        string
	ShippingMethod  string
	ProductName     string
	ProductCategory string
	ProductProfile  string
	ValueA          string // Tenant ID
	ValueB          string // Environment ID
	ValueC          string // Payment ID
	ValueD          string // Additional metadata
}

// ToMap converts the form data struct to a map for HTTP form submission
func (f *SSLCommerzFormData) ToMap() map[string]string {
	return map[string]string{
		"store_id":         f.StoreID,
		"store_passwd":     f.StorePassword,
		"total_amount":     f.TotalAmount,
		"currency":         f.Currency,
		"tran_id":          f.TranID,
		"success_url":      f.SuccessURL,
		"fail_url":         f.FailURL,
		"cancel_url":       f.CancelURL,
		"ipn_url":          f.IPNURL,
		"cus_name":         f.CusName,
		"cus_email":        f.CusEmail,
		"cus_add1":         f.CusAdd1,
		"cus_city":         f.CusCity,
		"cus_postcode":     f.CusPostcode,
		"cus_country":      f.CusCountry,
		"cus_phone":        f.CusPhone,
		"shipping_method":  f.ShippingMethod,
		"product_name":     f.ProductName,
		"product_category": f.ProductCategory,
		"product_profile":  f.ProductProfile,
		"value_a":          f.ValueA,
		"value_b":          f.ValueB,
		"value_c":          f.ValueC,
		"value_d":          f.ValueD,
	}
}

// SSLCommerzPaymentStatus represents SSLCommerz payment status values
type SSLCommerzPaymentStatus string

const (
	SSLCommerzStatusValid            SSLCommerzPaymentStatus = "VALID"
	SSLCommerzStatusValidated        SSLCommerzPaymentStatus = "VALIDATED"
	SSLCommerzStatusFailed           SSLCommerzPaymentStatus = "FAILED"
	SSLCommerzStatusCancelled        SSLCommerzPaymentStatus = "CANCELLED"
	SSLCommerzStatusExpired          SSLCommerzPaymentStatus = "EXPIRED"
	SSLCommerzStatusUnattempted      SSLCommerzPaymentStatus = "UNATTEMPTED"
	SSLCommerzStatusPendingVerify    SSLCommerzPaymentStatus = "PENDING_VERIFICATION"
	SSLCommerzStatusInvalidPaymentID SSLCommerzPaymentStatus = "INVALID_PAYMENT_ID"
)

// IsSuccess returns true if the status indicates a successful payment
func (s SSLCommerzPaymentStatus) IsSuccess() bool {
	return s == SSLCommerzStatusValid || s == SSLCommerzStatusValidated
}

// IsFailed returns true if the status indicates a failed payment
func (s SSLCommerzPaymentStatus) IsFailed() bool {
	return s == SSLCommerzStatusFailed || s == SSLCommerzStatusCancelled || s == SSLCommerzStatusExpired
}
