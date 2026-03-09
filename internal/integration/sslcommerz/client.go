package sslcommerz

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/connection"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/security"
	"github.com/flexprice/flexprice/internal/types"
)

// SSLCommerzClient defines the interface for SSLCommerz API operations
type SSLCommerzClient interface {
	GetSSLCommerzConfig(ctx context.Context) (*SSLCommerzConfig, error)
	GetDecryptedSSLCommerzConfig(conn *connection.Connection) (*SSLCommerzConfig, error)
	HasSSLCommerzConnection(ctx context.Context) bool
	GetConnection(ctx context.Context) (*connection.Connection, error)
	CreatePaymentSession(ctx context.Context, formData *SSLCommerzFormData) (*SSLCommerzAPIResponse, error)
	ValidatePaymentOrder(ctx context.Context, valID string) (*SSLCommerzValidationResponse, error)
	GetAppConfig() *config.Configuration
}

// Client handles SSLCommerz API client setup and configuration
type Client struct {
	connectionRepo    connection.Repository
	encryptionService security.EncryptionService
	logger            *logger.Logger
	httpClient        *http.Client
	config            *config.Configuration
}

// NewClient creates a new SSLCommerz client
func NewClient(
	connectionRepo connection.Repository,
	encryptionService security.EncryptionService,
	logger *logger.Logger,
	cfg *config.Configuration,
) SSLCommerzClient {
	return &Client{
		connectionRepo:    connectionRepo,
		encryptionService: encryptionService,
		logger:            logger,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		config: cfg,
	}
}

// GetAppConfig returns the application configuration
func (c *Client) GetAppConfig() *config.Configuration {
	return c.config
}

// GetSSLCommerzConfig retrieves and decrypts SSLCommerz configuration for the current environment
func (c *Client) GetSSLCommerzConfig(ctx context.Context) (*SSLCommerzConfig, error) {
	// Get SSLCommerz connection for this environment
	conn, err := c.connectionRepo.GetByProvider(ctx, types.SecretProviderSSLCommerz)
	if err != nil {
		return nil, ierr.NewError("failed to get SSLCommerz connection").
			WithHint("SSLCommerz connection not configured for this environment").
			Mark(ierr.ErrNotFound)
	}

	sslConfig, err := c.GetDecryptedSSLCommerzConfig(conn)
	if err != nil {
		return nil, ierr.NewError("failed to get SSLCommerz configuration").
			WithHint("Invalid SSLCommerz configuration").
			Mark(ierr.ErrValidation)
	}

	// Validate required fields
	if sslConfig.StoreID == "" {
		c.logger.Errorw("missing SSLCommerz store ID",
			"connection_id", conn.ID,
			"environment_id", conn.EnvironmentID)
		return nil, ierr.NewError("missing SSLCommerz store ID").
			WithHint("Configure SSLCommerz store ID in the connection settings").
			Mark(ierr.ErrValidation)
	}

	if sslConfig.StorePassword == "" {
		c.logger.Errorw("missing SSLCommerz store password",
			"connection_id", conn.ID,
			"environment_id", conn.EnvironmentID)
		return nil, ierr.NewError("missing SSLCommerz store password").
			WithHint("Configure SSLCommerz store password in the connection settings").
			Mark(ierr.ErrValidation)
	}

	// Add app config URLs to the config
	if c.config != nil {
		if sslConfig.BaseURL == "" && c.config.SSLCommerz.BaseURL != "" {
			sslConfig.BaseURL = c.config.SSLCommerz.BaseURL
		}
		if sslConfig.SessionAPI == "" && c.config.SSLCommerz.SessionAPI != "" {
			sslConfig.SessionAPI = c.config.SSLCommerz.SessionAPI
		}
		if sslConfig.ValidationAPI == "" && c.config.SSLCommerz.ValidationAPI != "" {
			sslConfig.ValidationAPI = c.config.SSLCommerz.ValidationAPI
		}
		if sslConfig.IPNURL == "" && c.config.SSLCommerz.IPNURL != "" {
			sslConfig.IPNURL = c.config.SSLCommerz.IPNURL
		}
		if sslConfig.SuccessURL == "" && c.config.SSLCommerz.SuccessURL != "" {
			sslConfig.SuccessURL = c.config.SSLCommerz.SuccessURL
		}
		if sslConfig.FailURL == "" && c.config.SSLCommerz.FailURL != "" {
			sslConfig.FailURL = c.config.SSLCommerz.FailURL
		}
		if sslConfig.CancelURL == "" && c.config.SSLCommerz.CancelURL != "" {
			sslConfig.CancelURL = c.config.SSLCommerz.CancelURL
		}
	}

	// Default to sandbox URL if not configured
	if sslConfig.BaseURL == "" {
		sslConfig.BaseURL = "https://sandbox.sslcommerz.com"
	}

	// Default to sandbox Session API if not configured
	if sslConfig.SessionAPI == "" {
		sslConfig.SessionAPI = sslConfig.BaseURL + "/gwprocess/v4/api.php"
	}

	// Default to sandbox Validation API if not configured
	if sslConfig.ValidationAPI == "" {
		sslConfig.ValidationAPI = sslConfig.BaseURL + "/validator/api/validationserverAPI.php"
	}

	return sslConfig, nil
}

// GetDecryptedSSLCommerzConfig decrypts and returns SSLCommerz configuration
func (c *Client) GetDecryptedSSLCommerzConfig(conn *connection.Connection) (*SSLCommerzConfig, error) {
	// Decrypt the connection metadata if it's encrypted
	decryptedMetadata, err := c.decryptConnectionMetadata(conn)
	if err != nil {
		return nil, err
	}

	// Extract SSLCommerz configuration from decrypted metadata
	sslConfig := &SSLCommerzConfig{}

	if storeID, exists := decryptedMetadata["store_id"]; exists {
		sslConfig.StoreID = storeID
	}

	if storePassword, exists := decryptedMetadata["store_password"]; exists {
		sslConfig.StorePassword = storePassword
	}

	return sslConfig, nil
}

// decryptConnectionMetadata decrypts the connection encrypted secret data
func (c *Client) decryptConnectionMetadata(conn *connection.Connection) (types.Metadata, error) {
	// Check if the connection has encrypted secret data
	if conn.EncryptedSecretData.SSLCommerz == nil {
		c.logger.Warnw("no sslcommerz metadata found in encrypted secret data", "connection_id", conn.ID)
		return types.Metadata{}, nil
	}

	// For SSLCommerz connections, decrypt the structured metadata
	if conn.ProviderType == types.SecretProviderSSLCommerz {

		// Decrypt each field
		storeID, err := c.encryptionService.Decrypt(conn.EncryptedSecretData.SSLCommerz.StoreID)
		if err != nil {
			c.logger.Errorw("failed to decrypt store ID", "connection_id", conn.ID, "error", err)
			return nil, ierr.NewError("failed to decrypt store ID").Mark(ierr.ErrInternal)
		}

		storePassword, err := c.encryptionService.Decrypt(conn.EncryptedSecretData.SSLCommerz.StorePassword)
		if err != nil {
			c.logger.Errorw("failed to decrypt store password", "connection_id", conn.ID, "error", err)
			return nil, ierr.NewError("failed to decrypt store password").Mark(ierr.ErrInternal)
		}

		decryptedMetadata := types.Metadata{
			"store_id":       storeID,
			"store_password": storePassword,
		}

		c.logger.Infow("successfully decrypted sslcommerz credentials",
			"connection_id", conn.ID,
			"has_store_id", storeID != "",
			"has_store_password", storePassword != "")

		return decryptedMetadata, nil
	}

	return types.Metadata{}, nil
}

// HasSSLCommerzConnection checks if the tenant has an SSLCommerz connection available
func (c *Client) HasSSLCommerzConnection(ctx context.Context) bool {
	conn, err := c.connectionRepo.GetByProvider(ctx, types.SecretProviderSSLCommerz)
	return err == nil && conn != nil && conn.Status == types.StatusPublished
}

// GetConnection retrieves the SSLCommerz connection for the current context
func (c *Client) GetConnection(ctx context.Context) (*connection.Connection, error) {
	conn, err := c.connectionRepo.GetByProvider(ctx, types.SecretProviderSSLCommerz)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get SSLCommerz connection").
			Mark(ierr.ErrDatabase)
	}
	if conn == nil {
		return nil, ierr.NewError("SSLCommerz connection not found").
			WithHint("SSLCommerz connection not configured for this environment").
			Mark(ierr.ErrNotFound)
	}
	return conn, nil
}

// CreatePaymentSession creates a payment session with SSLCommerz
func (c *Client) CreatePaymentSession(ctx context.Context, formData *SSLCommerzFormData) (*SSLCommerzAPIResponse, error) {
	sslConfig, err := c.GetSSLCommerzConfig(ctx)
	if err != nil {
		return nil, err
	}

	// Set credentials from config
	formData.StoreID = sslConfig.StoreID
	formData.StorePassword = sslConfig.StorePassword

	// Set IPN URL if not already set
	if formData.IPNURL == "" && sslConfig.IPNURL != "" {
		formData.IPNURL = sslConfig.IPNURL
	}

	paymentURL := sslConfig.SessionAPI

	// Build form data
	data := url.Values{}
	for key, value := range formData.ToMap() {
		data.Set(key, value)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, paymentURL, strings.NewReader(data.Encode()))
	if err != nil {
		c.logger.Errorw("failed to create SSLCommerz request",
			"error", err,
			"tran_id", formData.TranID)
		return nil, ierr.NewError("failed to create payment session request").
			Mark(ierr.ErrInternal)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Errorw("SSLCommerz API request failed",
			"error", err,
			"tran_id", formData.TranID)
		return nil, ierr.NewError("failed to create payment session").
			WithHint("SSLCommerz API request failed").
			Mark(ierr.ErrInternal)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.logger.Errorw("failed to read SSLCommerz response",
			"error", err,
			"tran_id", formData.TranID)
		return nil, ierr.NewError("failed to read payment session response").
			Mark(ierr.ErrInternal)
	}

	if resp.StatusCode != http.StatusOK {
		c.logger.Errorw("SSLCommerz API returned error status",
			"status_code", resp.StatusCode,
			"tran_id", formData.TranID,
			"response", string(body))
		return nil, ierr.NewError("failed to create payment session").
			WithHint("SSLCommerz API returned an error").
			WithReportableDetails(map[string]interface{}{
				"status_code": resp.StatusCode,
				"response":    string(body),
			}).
			Mark(ierr.ErrInternal)
	}

	var response SSLCommerzAPIResponse
	if err := json.Unmarshal(body, &response); err != nil {
		c.logger.Errorw("failed to parse SSLCommerz response",
			"error", err,
			"tran_id", formData.TranID,
			"response", string(body))
		return nil, ierr.NewError("failed to parse payment session response").
			Mark(ierr.ErrInternal)
	}

	if response.Status == "FAILED" {
		c.logger.Errorw("SSLCommerz payment session creation failed",
			"failed_reason", response.FailedReason,
			"tran_id", formData.TranID)
		return nil, ierr.NewError("failed to create payment session").
			WithHint(response.FailedReason).
			Mark(ierr.ErrInternal)
	}

	c.logger.Infow("successfully created SSLCommerz payment session",
		"tran_id", formData.TranID,
		"session_key", response.SessionKey)

	return &response, nil
}

// ValidatePaymentOrder validates a payment order with SSLCommerz
func (c *Client) ValidatePaymentOrder(ctx context.Context, valID string) (*SSLCommerzValidationResponse, error) {
	sslConfig, err := c.GetSSLCommerzConfig(ctx)
	if err != nil {
		return nil, err
	}

	validationURL := sslConfig.ValidationAPI

	// Build query parameters
	params := url.Values{}
	params.Set("val_id", valID)
	params.Set("store_id", sslConfig.StoreID)
	params.Set("store_passwd", sslConfig.StorePassword)
	params.Set("v", "1")
	params.Set("format", "json")

	fullURL := validationURL + "?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		c.logger.Errorw("failed to create SSLCommerz validation request",
			"error", err,
			"val_id", valID)
		return nil, ierr.NewError("failed to create validation request").
			Mark(ierr.ErrInternal)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Errorw("SSLCommerz validation API request failed",
			"error", err,
			"val_id", valID)
		return nil, ierr.NewError("failed to validate payment order").
			WithHint("SSLCommerz validation API request failed").
			Mark(ierr.ErrInternal)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.logger.Errorw("failed to read SSLCommerz validation response",
			"error", err,
			"val_id", valID)
		return nil, ierr.NewError("failed to read validation response").
			Mark(ierr.ErrInternal)
	}

	if resp.StatusCode != http.StatusOK {
		c.logger.Errorw("SSLCommerz validation API returned error status",
			"status_code", resp.StatusCode,
			"val_id", valID,
			"response", string(body))
		return nil, ierr.NewError("failed to validate payment order").
			WithHint("SSLCommerz validation API returned an error").
			Mark(ierr.ErrInternal)
	}

	var response SSLCommerzValidationResponse
	if err := json.Unmarshal(body, &response); err != nil {
		c.logger.Errorw("failed to parse SSLCommerz validation response",
			"error", err,
			"val_id", valID,
			"response", string(body))
		return nil, ierr.NewError("failed to parse validation response").
			Mark(ierr.ErrInternal)
	}

	c.logger.Infow("successfully validated SSLCommerz payment order",
		"val_id", valID,
		"status", response.Status,
		"tran_id", response.TranID)

	return &response, nil
}
