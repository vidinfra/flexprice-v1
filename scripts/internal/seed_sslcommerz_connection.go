package internal

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/flexprice/flexprice/ent/connection"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/security"
	"github.com/flexprice/flexprice/internal/sentry"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/oklog/ulid/v2"
)

// SeedSSLCommerzConnection seeds an SSLCommerz connection for a tenant/environment
func SeedSSLCommerzConnection() error {
	// Get parameters from environment variables
	tenantID := os.Getenv("TENANT_ID")
	environmentID := os.Getenv("ENVIRONMENT_ID")
	storeID := os.Getenv("SSLCOMMERZ_STORE_ID")
	storePassword := os.Getenv("SSLCOMMERZ_STORE_PASSWORD")
	connectionName := os.Getenv("CONNECTION_NAME")

	// Validate required parameters
	if tenantID == "" {
		return fmt.Errorf("TENANT_ID is required")
	}
	if environmentID == "" {
		return fmt.Errorf("ENVIRONMENT_ID is required")
	}
	if storeID == "" {
		return fmt.Errorf("SSLCOMMERZ_STORE_ID is required")
	}
	if storePassword == "" {
		return fmt.Errorf("SSLCOMMERZ_STORE_PASSWORD is required")
	}
	if connectionName == "" {
		connectionName = "SSLCommerz Connection"
	}

	fmt.Printf("Seeding SSLCommerz connection...\n")
	fmt.Printf("  Tenant ID: %s\n", tenantID)
	fmt.Printf("  Environment ID: %s\n", environmentID)
	fmt.Printf("  Store ID: %s\n", storeID)
	fmt.Printf("  Connection Name: %s\n", connectionName)

	// Load configuration
	cfg, err := config.NewConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Create logger
	log, err := logger.NewLogger(cfg)
	if err != nil {
		return fmt.Errorf("failed to create logger: %w", err)
	}

	// Create encryption service
	encryptionService, err := security.NewEncryptionService(cfg, log)
	if err != nil {
		return fmt.Errorf("failed to create encryption service: %w", err)
	}

	// Encrypt credentials
	encryptedStoreID, err := encryptionService.Encrypt(storeID)
	if err != nil {
		return fmt.Errorf("failed to encrypt store ID: %w", err)
	}

	encryptedStorePassword, err := encryptionService.Encrypt(storePassword)
	if err != nil {
		return fmt.Errorf("failed to encrypt store password: %w", err)
	}

	fmt.Printf("  Credentials encrypted successfully\n")

	// Create database client
	ctx := context.Background()
	entClient, err := postgres.NewEntClients(cfg, log)
	if err != nil {
		return fmt.Errorf("failed to connect to postgres: %w", err)
	}
	defer entClient.Writer.Close()

	dbClient := postgres.NewClient(entClient, log, sentry.NewSentryService(cfg, log))

	// Generate connection ID
	connectionID := fmt.Sprintf("conn_%s", ulid.Make().String())

	// Build encrypted_secret_data map
	encryptedSecretData := map[string]interface{}{
		"store_id":       encryptedStoreID,
		"store_password": encryptedStorePassword,
	}

	now := time.Now().UTC()

	// Check if connection already exists for this tenant/environment/provider
	existingConn, err := dbClient.Writer(ctx).Connection.Query().
		Where(
			connection.TenantID(tenantID),
			connection.EnvironmentID(environmentID),
			connection.ProviderType(string(types.SecretProviderSSLCommerz)),
			connection.Status(string(types.StatusPublished)),
		).
		First(ctx)

	if err == nil && existingConn != nil {
		fmt.Printf("\nSSLCommerz connection already exists (ID: %s). Updating credentials...\n", existingConn.ID)

		// Update existing connection
		_, err := dbClient.Writer(ctx).Connection.UpdateOneID(existingConn.ID).
			SetEncryptedSecretData(encryptedSecretData).
			SetName(connectionName).
			SetUpdatedAt(now).
			SetUpdatedBy("system").
			Save(ctx)

		if err != nil {
			return fmt.Errorf("failed to update connection: %w", err)
		}

		fmt.Printf("\nSSLCommerz connection updated successfully!\n")
		fmt.Printf("  Connection ID: %s\n", existingConn.ID)
		return nil
	}

	// Insert new connection
	_, err = dbClient.Writer(ctx).Connection.Create().
		SetID(connectionID).
		SetTenantID(tenantID).
		SetEnvironmentID(environmentID).
		SetName(connectionName).
		SetProviderType(string(types.SecretProviderSSLCommerz)).
		SetEncryptedSecretData(encryptedSecretData).
		SetStatus(string(types.StatusPublished)).
		SetCreatedAt(now).
		SetUpdatedAt(now).
		SetCreatedBy("system").
		SetUpdatedBy("system").
		Save(ctx)

	if err != nil {
		return fmt.Errorf("failed to create connection: %w", err)
	}

	fmt.Printf("\nSSLCommerz connection created successfully!\n")
	fmt.Printf("  Connection ID: %s\n", connectionID)

	return nil
}
