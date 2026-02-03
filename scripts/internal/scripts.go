package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/flexprice/flexprice/internal/auth"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/environment"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/repository"
	"github.com/flexprice/flexprice/internal/sentry"
	"github.com/flexprice/flexprice/internal/types"
)

// GenerateNewAPIKey generates a new API key
func GenerateNewAPIKey() error {
	// Generate a new API key
	rawKey := auth.GenerateAPIKey()
	hashedKey := auth.HashAPIKey(rawKey)

	userID := os.Getenv("USER_ID")
	tenantID := os.Getenv("TENANT_ID")

	// Create API key details (customize these values)
	details := config.APIKeyDetails{
		TenantID: tenantID,
		UserID:   userID,
		Name:     "Dev API Keys",
		IsActive: true,
	}

	// Create the configuration map
	keysMap := map[string]config.APIKeyDetails{
		hashedKey: details,
	}

	// Convert to JSON
	jsonBytes, err := json.Marshal(keysMap)
	if err != nil {
		return err
	}

	fmt.Printf("\nNew API Key Generated:\n")
	fmt.Printf("Raw Key (give this to your customer): %s\n", rawKey)
	fmt.Printf("\nConfiguration:\n")
	fmt.Printf("Add this to your config.yaml under auth.api_key.keys:\n")
	fmt.Printf("%s:\n", hashedKey)
	fmt.Printf("  tenant_id: %s\n", details.TenantID)
	fmt.Printf("  user_id: %s\n", details.UserID)
	fmt.Printf("  name: %s\n", details.Name)
	fmt.Printf("  is_active: %v\n", details.IsActive)
	fmt.Printf("\nOr set this environment variable:\n")
	fmt.Printf("FLEXPRICE_AUTH_API_KEY_KEYS='%s'\n", string(jsonBytes))

	return nil
}

// AssignTenantToUser assigns a tenant to a user
func AssignTenantToUser() error {
	userID := os.Getenv("USER_ID")
	tenantID := os.Getenv("TENANT_ID")
	// Load configuration
	cfg, err := config.NewConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
		return err
	}

	// Create auth provider
	authProvider := auth.NewProvider(cfg)

	// Assign tenant to user
	err = authProvider.AssignUserToTenant(context.Background(), userID, tenantID)
	if err != nil {
		log.Fatalf("Failed to assign tenant to user: %v", err)
		return err
	}

	fmt.Printf("Successfully assigned tenant %s to user %s\n", tenantID, userID)
	return nil
}

// CreateTenantWithAPIKey creates a new tenant with environments and generates an API key
func CreateTenantWithAPIKey() error {
	tenantName := "tenbyte"

	// Load configuration
	cfg, err := config.NewConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Initialize logger
	logr, err := logger.NewLogger(cfg)
	if err != nil {
		return fmt.Errorf("failed to create logger: %w", err)
	}

	// Initialize the DB
	entClient, err := postgres.NewEntClients(cfg, logr)
	if err != nil {
		return fmt.Errorf("failed to connect to postgres: %w", err)
	}
	client := postgres.NewClient(entClient, logr, sentry.NewSentryService(cfg, logr))

	// Initialize repositories
	repoParams := repository.RepositoryParams{
		EntClient: client,
		Logger:    logr,
	}

	tenantRepo := repository.NewTenantRepository(repoParams)
	environmentRepo := repository.NewEnvironmentRepository(repoParams)

	ctx := context.Background()

	// Create tenant
	tenantID := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TENANT)
	t := &tenant.Tenant{
		ID:        tenantID,
		Name:      tenantName,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := tenantRepo.Create(ctx, t); err != nil {
		return fmt.Errorf("failed to create tenant: %w", err)
	}

	logr.Infow("created tenant", "id", t.ID, "name", t.Name)

	// Create default environments (sandbox + production)
	envTypes := []types.EnvironmentType{
		types.EnvironmentDevelopment,
		types.EnvironmentProduction,
	}

	envNameMap := map[types.EnvironmentType]string{
		types.EnvironmentDevelopment: "Sandbox",
		types.EnvironmentProduction:  "Production",
	}

	for _, envType := range envTypes {
		e := &environment.Environment{
			ID:   types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENVIRONMENT),
			Name: envNameMap[envType],
			Type: envType,
			BaseModel: types.BaseModel{
				TenantID:  tenantID,
				CreatedBy: types.DefaultUserID,
				UpdatedBy: types.DefaultUserID,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
		}

		if err := environmentRepo.Create(ctx, e); err != nil {
			return fmt.Errorf("failed to create environment %s: %w", envType, err)
		}
		logr.Infow("created environment", "id", e.ID, "name", e.Name, "type", e.Type)
	}

	// Generate API key
	rawKey := auth.GenerateAPIKey()
	hashedKey := auth.HashAPIKey(rawKey)

	// Use default user ID for API key
	userID := types.DefaultUserID

	// Create API key details
	details := config.APIKeyDetails{
		TenantID: tenantID,
		UserID:   userID,
		Name:     fmt.Sprintf("%s API Key", tenantName),
		IsActive: true,
	}

	// Create the configuration map
	keysMap := map[string]config.APIKeyDetails{
		hashedKey: details,
	}

	// Convert to JSON
	jsonBytes, err := json.Marshal(keysMap)
	if err != nil {
		return fmt.Errorf("failed to marshal API key config: %w", err)
	}

	fmt.Println("\n============================================================")
	fmt.Println("TENANT CREATED SUCCESSFULLY")
	fmt.Println("============================================================")
	fmt.Printf("\nTenant Name: %s\n", tenantName)
	fmt.Printf("Tenant ID:   %s\n", tenantID)
	fmt.Printf("User ID:     %s\n", userID)

	fmt.Println("\n------------------------------------------------------------")
	fmt.Println("API KEY (save this - it won't be shown again!)")
	fmt.Println("------------------------------------------------------------")
	fmt.Printf("\nRaw API Key: %s\n", rawKey)

	fmt.Println("\n------------------------------------------------------------")
	fmt.Println("CONFIG.YAML FORMAT")
	fmt.Println("------------------------------------------------------------")
	fmt.Printf("\nAdd this under auth.api_key.keys:\n\n")
	fmt.Printf("%s:\n", hashedKey)
	fmt.Printf("  tenant_id: %s\n", details.TenantID)
	fmt.Printf("  user_id: %s\n", details.UserID)
	fmt.Printf("  name: \"%s\"\n", details.Name)
	fmt.Printf("  is_active: %v\n", details.IsActive)

	fmt.Println("\n------------------------------------------------------------")
	fmt.Println("ENVIRONMENT VARIABLE FORMAT")
	fmt.Println("------------------------------------------------------------")
	fmt.Printf("\nFLEXPRICE_AUTH_API_KEY_KEYS='%s'\n", string(jsonBytes))

	fmt.Println("\n============================================================")

	return nil
}
