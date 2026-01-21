package webhook

// SSLCommerzIPNData represents the IPN (Instant Payment Notification) data from SSLCommerz
type SSLCommerzIPNData struct {
	// Transaction details
	TranID     string `form:"tran_id" json:"tran_id"`         // Transaction ID (invoice_id)
	ValID      string `form:"val_id" json:"val_id"`           // Validation ID
	Status     string `form:"status" json:"status"`           // Payment status
	Amount     string `form:"amount" json:"amount"`           // Transaction amount
	Currency   string `form:"currency" json:"currency"`       // Currency code
	StoreAmount string `form:"store_amount" json:"store_amount"` // Store amount after fees

	// Bank/Card details
	BankTranID        string `form:"bank_tran_id" json:"bank_tran_id"`               // Bank transaction ID
	CardType          string `form:"card_type" json:"card_type"`                     // Card type
	CardNo            string `form:"card_no" json:"card_no"`                         // Masked card number
	CardIssuer        string `form:"card_issuer" json:"card_issuer"`                 // Card issuer bank
	CardBrand         string `form:"card_brand" json:"card_brand"`                   // Card brand (VISA, MC, etc.)
	CardIssuerCountry string `form:"card_issuer_country" json:"card_issuer_country"` // Card issuer country

	// Risk assessment
	RiskLevel string `form:"risk_level" json:"risk_level"` // Risk level (0 = safe, 1 = risky)
	RiskTitle string `form:"risk_title" json:"risk_title"` // Risk title

	// Custom metadata (passed in value_a, value_b, value_c, value_d)
	ValueA string `form:"value_a" json:"value_a"` // Tenant ID
	ValueB string `form:"value_b" json:"value_b"` // Environment ID
	ValueC string `form:"value_c" json:"value_c"` // Payment ID
	ValueD string `form:"value_d" json:"value_d"` // Additional metadata

	// EMI details (if applicable)
	EmiInstalment  string `form:"emi_instalment" json:"emi_instalment"`
	EmiAmount      string `form:"emi_amount" json:"emi_amount"`
	EmiDescription string `form:"emi_description" json:"emi_description"`
	EmiIssuer      string `form:"emi_issuer" json:"emi_issuer"`

	// Currency conversion details
	CurrencyType   string `form:"currency_type" json:"currency_type"`
	CurrencyAmount string `form:"currency_amount" json:"currency_amount"`
	CurrencyRate   string `form:"currency_rate" json:"currency_rate"`

	// Transaction date
	TranDate string `form:"tran_date" json:"tran_date"`

	// Verification fields
	VerifySign     string `form:"verify_sign" json:"verify_sign"`
	VerifySignSHA2 string `form:"verify_sign_sha2" json:"verify_sign_sha2"`
	VerifyKey      string `form:"verify_key" json:"verify_key"`
}

// SSLCommerzRedirectData represents the redirect callback data from SSLCommerz
type SSLCommerzRedirectData struct {
	TranID   string `form:"tran_id" query:"tran_id" json:"tran_id"`
	ValID    string `form:"val_id" query:"val_id" json:"val_id"`
	Status   string `form:"status" query:"status" json:"status"`
	Amount   string `form:"amount" query:"amount" json:"amount"`
	Currency string `form:"currency" query:"currency" json:"currency"`
}

// SSLCommerzPaymentStatus represents possible payment status values from SSLCommerz
type SSLCommerzPaymentStatus string

const (
	StatusValid            SSLCommerzPaymentStatus = "VALID"
	StatusValidated        SSLCommerzPaymentStatus = "VALIDATED"
	StatusFailed           SSLCommerzPaymentStatus = "FAILED"
	StatusCancelled        SSLCommerzPaymentStatus = "CANCELLED"
	StatusExpired          SSLCommerzPaymentStatus = "EXPIRED"
	StatusUnattempted      SSLCommerzPaymentStatus = "UNATTEMPTED"
	StatusPendingVerify    SSLCommerzPaymentStatus = "PENDING_VERIFICATION"
)

// IsSuccess returns true if the status indicates a successful payment
func (s SSLCommerzPaymentStatus) IsSuccess() bool {
	return s == StatusValid || s == StatusValidated
}

// IsFailed returns true if the status indicates a failed payment
func (s SSLCommerzPaymentStatus) IsFailed() bool {
	return s == StatusFailed || s == StatusCancelled || s == StatusExpired
}

// IsPending returns true if the status indicates a pending payment
func (s SSLCommerzPaymentStatus) IsPending() bool {
	return s == StatusUnattempted || s == StatusPendingVerify
}
