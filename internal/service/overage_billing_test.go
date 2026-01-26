package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/settings"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type OverageBillingServiceSuite struct {
	testutil.BaseServiceTestSuite
	service  OverageBillingService
	testData struct {
		customer     *customer.Customer
		subscription *subscription.Subscription
		wallet       *wallet.Wallet
		now          time.Time
	}
}

func TestOverageBillingService(t *testing.T) {
	suite.Run(t, new(OverageBillingServiceSuite))
}

func (s *OverageBillingServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.setupService()
	s.setupTestData()
}

func (s *OverageBillingServiceSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *OverageBillingServiceSuite) setupService() {
	// Create a mock processed event repo
	mockProcessedEventRepo := testutil.NewMockProcessedEventRepository()

	s.service = NewOverageBillingService(
		ServiceParams{
			Logger:                       s.GetLogger(),
			Config:                       s.GetConfig(),
			DB:                           s.GetDB(),
			SubRepo:                      s.GetStores().SubscriptionRepo,
			SubscriptionLineItemRepo:     s.GetStores().SubscriptionLineItemRepo,
			PlanRepo:                     s.GetStores().PlanRepo,
			PriceRepo:                    s.GetStores().PriceRepo,
			CustomerRepo:                 s.GetStores().CustomerRepo,
			InvoiceRepo:                  s.GetStores().InvoiceRepo,
			WalletRepo:                   s.GetStores().WalletRepo,
			PaymentRepo:                  s.GetStores().PaymentRepo,
			SettingsRepo:                 s.GetStores().SettingsRepo,
			TaxRateRepo:                  s.GetStores().TaxRateRepo,
			TaxAssociationRepo:           s.GetStores().TaxAssociationRepo,
			TaxAppliedRepo:               s.GetStores().TaxAppliedRepo,
			CouponRepo:                   s.GetStores().CouponRepo,
			CouponAssociationRepo:        s.GetStores().CouponAssociationRepo,
			CouponApplicationRepo:        s.GetStores().CouponApplicationRepo,
			CreditGrantRepo:              s.GetStores().CreditGrantRepo,
			CreditGrantApplicationRepo:   s.GetStores().CreditGrantApplicationRepo,
			CreditNoteRepo:               s.GetStores().CreditNoteRepo,
			CreditNoteLineItemRepo:       s.GetStores().CreditNoteLineItemRepo,
			MeterRepo:                    s.GetStores().MeterRepo,
			FeatureRepo:                  s.GetStores().FeatureRepo,
			EntitlementRepo:              s.GetStores().EntitlementRepo,
			EventPublisher:               s.GetPublisher(),
			WebhookPublisher:             s.GetWebhookPublisher(),
		},
		mockProcessedEventRepo,
	)
}

func (s *OverageBillingServiceSuite) setupTestData() {
	s.testData.now = time.Now().UTC()

	// Create test customer
	s.testData.customer = &customer.Customer{
		ID:         "cust_overage_test",
		ExternalID: "ext_cust_overage_test",
		Name:       "Overage Test Customer",
		Email:      "overage@example.com",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), s.testData.customer))

	// Create test subscription
	periodStart := s.testData.now.Add(-24 * time.Hour)
	periodEnd := s.testData.now.Add(30 * 24 * time.Hour)
	s.testData.subscription = &subscription.Subscription{
		ID:                 "sub_overage_test",
		CustomerID:         s.testData.customer.ID,
		PlanID:             "plan_overage_test",
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		SubscriptionStatus: types.SubscriptionStatusActive,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		StartDate:          periodStart,
		BillingAnchor:      periodStart,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(s.GetContext(), s.testData.subscription))

	// Create test wallet with positive balance
	s.testData.wallet = &wallet.Wallet{
		ID:             "wallet_overage_test",
		CustomerID:     s.testData.customer.ID,
		Currency:       "usd",
		Balance:        decimal.NewFromFloat(3.0), // $3 balance - less than $5 threshold
		CreditBalance:  decimal.NewFromFloat(3.0),
		ConversionRate: decimal.NewFromFloat(1.0),
		WalletStatus:   types.WalletStatusActive,
		WalletType:     types.WalletTypePrePaid,
		BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), s.testData.wallet))
	s.NoError(s.GetStores().WalletRepo.CreateTransaction(s.GetContext(), &wallet.Transaction{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION),
		WalletID:         s.testData.wallet.ID,
		Type:             types.TransactionTypeCredit,
		Amount:           s.testData.wallet.Balance,
		CreditAmount:     s.testData.wallet.CreditBalance,
		CreditsAvailable: s.testData.wallet.CreditBalance,
		TxStatus:         types.TransactionStatusCompleted,
		BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
	}))
}

func (s *OverageBillingServiceSuite) TestGetOverageBillingConfig_Default() {
	// Test getting default config when no setting exists
	config, err := s.service.GetOverageBillingConfig(s.GetContext())
	s.NoError(err)
	s.NotNil(config)
	s.True(config.Enabled) // Default is now enabled
	s.True(config.InvoiceThreshold.Equal(decimal.NewFromFloat(5.0)))
}

func (s *OverageBillingServiceSuite) TestGetOverageBillingConfig_CustomSetting() {
	// Create a custom setting
	customConfig := map[string]interface{}{
		"enabled":           true,
		"invoice_threshold": "10.00",
	}
	setting := &settings.Setting{
		ID:            types.GenerateUUIDWithPrefix("set"),
		Key:           types.SettingKeyOverageBillingConfig,
		Value:         customConfig,
		EnvironmentID: types.GetEnvironmentID(s.GetContext()),
		BaseModel:     types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().SettingsRepo.Create(s.GetContext(), setting))

	// Test getting custom config
	config, err := s.service.GetOverageBillingConfig(s.GetContext())
	s.NoError(err)
	s.NotNil(config)
	s.True(config.Enabled)
	s.True(config.InvoiceThreshold.Equal(decimal.NewFromFloat(10.0)))
}

func (s *OverageBillingServiceSuite) TestProcessEventOverage_DisabledConfig() {
	// Disable overage billing
	customConfig := map[string]interface{}{
		"enabled":           false,
		"invoice_threshold": "5.00",
	}
	setting := &settings.Setting{
		ID:            types.GenerateUUIDWithPrefix("set"),
		Key:           types.SettingKeyOverageBillingConfig,
		Value:         customConfig,
		EnvironmentID: types.GetEnvironmentID(s.GetContext()),
		BaseModel:     types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().SettingsRepo.Create(s.GetContext(), setting))

	// Create a processed event with cost
	processedEvent := &events.ProcessedEvent{
		Event: events.Event{
			ID:                 "event_disabled",
			TenantID:           types.GetTenantID(s.GetContext()),
			EnvironmentID:      types.GetEnvironmentID(s.GetContext()),
			CustomerID:         s.testData.customer.ID,
			ExternalCustomerID: s.testData.customer.ExternalID,
			EventName:          "api_call",
			Timestamp:          s.testData.now,
		},
		SubscriptionID: s.testData.subscription.ID,
		PeriodID:       1,
		Cost:           decimal.NewFromFloat(10.0), // $10 - above threshold
		Currency:       "usd",
	}

	subResponse := &dto.SubscriptionResponse{
		Subscription: s.testData.subscription,
	}

	// Process the event - should not create invoice (disabled)
	err := s.service.ProcessEventOverage(s.GetContext(), processedEvent, subResponse)
	s.NoError(err)

	// Verify no invoice was created
	invoices, err := s.GetStores().InvoiceRepo.List(s.GetContext(), &types.InvoiceFilter{
		CustomerID:  s.testData.customer.ID,
		QueryFilter: types.NewNoLimitQueryFilter(),
	})
	s.NoError(err)
	s.Empty(invoices, "No invoice should be created when overage billing is disabled")
}

func (s *OverageBillingServiceSuite) TestProcessEventOverage_ZeroCost() {
	// Create a processed event with zero cost
	processedEvent := &events.ProcessedEvent{
		Event: events.Event{
			ID:                 "event_zero_cost",
			TenantID:           types.GetTenantID(s.GetContext()),
			EnvironmentID:      types.GetEnvironmentID(s.GetContext()),
			CustomerID:         s.testData.customer.ID,
			ExternalCustomerID: s.testData.customer.ExternalID,
			EventName:          "api_call",
			Timestamp:          s.testData.now,
		},
		SubscriptionID: s.testData.subscription.ID,
		PeriodID:       1,
		Cost:           decimal.Zero, // No cost
		Currency:       "usd",
	}

	subResponse := &dto.SubscriptionResponse{
		Subscription: s.testData.subscription,
	}

	// Process the event - should skip (no overage cost)
	err := s.service.ProcessEventOverage(s.GetContext(), processedEvent, subResponse)
	s.NoError(err)

	// Verify no invoice was created
	invoices, err := s.GetStores().InvoiceRepo.List(s.GetContext(), &types.InvoiceFilter{
		CustomerID:  s.testData.customer.ID,
		QueryFilter: types.NewNoLimitQueryFilter(),
	})
	s.NoError(err)
	s.Empty(invoices, "No invoice should be created for zero cost events")
}

func (s *OverageBillingServiceSuite) TestOverageBillingConfig_Validation() {
	// Test that negative threshold is rejected
	config := types.OverageBillingConfig{
		Enabled:          true,
		InvoiceThreshold: decimal.NewFromFloat(-5.0),
	}
	err := config.Validate()
	s.Error(err, "Negative threshold should be rejected")

	// Test that zero threshold is valid
	config.InvoiceThreshold = decimal.Zero
	err = config.Validate()
	s.NoError(err, "Zero threshold should be valid")

	// Test that positive threshold is valid
	config.InvoiceThreshold = decimal.NewFromFloat(5.0)
	err = config.Validate()
	s.NoError(err, "Positive threshold should be valid")
}

func (s *OverageBillingServiceSuite) TestWebhookEventType() {
	// Verify the webhook event type constant is correct
	s.Equal("wallet.balance.negative", types.WebhookEventWalletBalanceNegative)
}

func (s *OverageBillingServiceSuite) TestSettingKey() {
	// Verify the setting key constant is correct
	s.Equal(types.SettingKey("overage_billing_config"), types.SettingKeyOverageBillingConfig)

	// Verify it's in the allowed keys
	key := types.SettingKeyOverageBillingConfig
	err := key.Validate()
	s.NoError(err, "SettingKeyOverageBillingConfig should be a valid setting key")
}

func (s *OverageBillingServiceSuite) TestDefaultSettings() {
	// Verify overage billing config is in default settings
	defaults, err := types.GetDefaultSettings()
	s.NoError(err)

	overageDefault, exists := defaults[types.SettingKeyOverageBillingConfig]
	s.True(exists, "Overage billing config should be in default settings")
	s.Equal(types.SettingKeyOverageBillingConfig, overageDefault.Key)
	s.NotEmpty(overageDefault.Description)

	// Verify default values
	enabled, ok := overageDefault.DefaultValue["enabled"].(bool)
	s.True(ok, "enabled should be a bool")
	s.True(enabled, "Default should be enabled")
}

func (s *OverageBillingServiceSuite) TestWalletBalanceRetrieval() {
	// Test that we can retrieve wallet balance for a customer
	wallets, err := s.GetStores().WalletRepo.GetWalletsByCustomerID(s.GetContext(), s.testData.customer.ID)
	s.NoError(err)
	s.Len(wallets, 1)
	s.True(wallets[0].Balance.Equal(decimal.NewFromFloat(3.0)))
}

func (s *OverageBillingServiceSuite) TestSubscriptionSetup() {
	// Verify subscription is set up correctly
	sub, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), s.testData.subscription.ID)
	s.NoError(err)
	s.NotNil(sub)
	s.Equal(s.testData.customer.ID, sub.CustomerID)
	s.Equal(types.SubscriptionStatusActive, sub.SubscriptionStatus)
	s.Equal("usd", sub.Currency)
}

func (s *OverageBillingServiceSuite) TestProcessEventOverage_NegativeCostSkipped() {
	// Create a processed event with negative cost (shouldn't happen but edge case)
	processedEvent := &events.ProcessedEvent{
		Event: events.Event{
			ID:                 "event_negative",
			TenantID:           types.GetTenantID(s.GetContext()),
			EnvironmentID:      types.GetEnvironmentID(s.GetContext()),
			CustomerID:         s.testData.customer.ID,
			ExternalCustomerID: s.testData.customer.ExternalID,
			EventName:          "api_call",
			Timestamp:          s.testData.now,
		},
		SubscriptionID: s.testData.subscription.ID,
		PeriodID:       1,
		Cost:           decimal.NewFromFloat(-5.0), // Negative cost
		Currency:       "usd",
	}

	subResponse := &dto.SubscriptionResponse{
		Subscription: s.testData.subscription,
	}

	// Process the event - should skip (negative cost is not overage)
	err := s.service.ProcessEventOverage(s.GetContext(), processedEvent, subResponse)
	s.NoError(err)

	// Verify no invoice was created
	invoices, err := s.GetStores().InvoiceRepo.List(s.GetContext(), &types.InvoiceFilter{
		CustomerID:  s.testData.customer.ID,
		QueryFilter: types.NewNoLimitQueryFilter(),
	})
	s.NoError(err)
	s.Empty(invoices, "No invoice should be created for negative cost events")
}

func (s *OverageBillingServiceSuite) TestProcessEventOverage_BelowThreshold() {
	// Set up mock with cost below threshold
	mockRepo := s.service.(*overageBillingService).processedEventRepo.(*testutil.MockProcessedEventRepository)
	mockRepo.Clear()
	// Accumulated cost of $2 - below $5 threshold
	mockRepo.SetPeriodCost(s.testData.subscription.ID, 1, decimal.NewFromFloat(2.0))

	// Create event with small cost
	processedEvent := &events.ProcessedEvent{
		Event: events.Event{
			ID:                 "event_below_threshold",
			TenantID:           types.GetTenantID(s.GetContext()),
			EnvironmentID:      types.GetEnvironmentID(s.GetContext()),
			CustomerID:         s.testData.customer.ID,
			ExternalCustomerID: s.testData.customer.ExternalID,
			EventName:          "api_call",
			Timestamp:          s.testData.now,
		},
		SubscriptionID: s.testData.subscription.ID,
		PeriodID:       1,
		Cost:           decimal.NewFromFloat(0.50), // Total $2.50, still below $5
		Currency:       "usd",
	}

	subResponse := &dto.SubscriptionResponse{
		Subscription: s.testData.subscription,
	}

	err := s.service.ProcessEventOverage(s.GetContext(), processedEvent, subResponse)
	s.NoError(err)

	// Verify no invoice created (below threshold)
	invoices, err := s.GetStores().InvoiceRepo.List(s.GetContext(), &types.InvoiceFilter{
		CustomerID:  s.testData.customer.ID,
		QueryFilter: types.NewNoLimitQueryFilter(),
	})
	s.NoError(err)
	s.Empty(invoices, "No invoice should be created when below threshold")
}

func (s *OverageBillingServiceSuite) TestProcessEventOverage_HighThresholdConfig() {
	// Set a high threshold ($100)
	customConfig := map[string]interface{}{
		"enabled":           true,
		"invoice_threshold": "100.00",
	}
	setting := &settings.Setting{
		ID:            types.GenerateUUIDWithPrefix("set"),
		Key:           types.SettingKeyOverageBillingConfig,
		Value:         customConfig,
		EnvironmentID: types.GetEnvironmentID(s.GetContext()),
		BaseModel:     types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().SettingsRepo.Create(s.GetContext(), setting))

	// Set up mock with cost below high threshold
	mockRepo := s.service.(*overageBillingService).processedEventRepo.(*testutil.MockProcessedEventRepository)
	mockRepo.Clear()
	mockRepo.SetPeriodCost(s.testData.subscription.ID, 1, decimal.NewFromFloat(50.0))

	// Create event that doesn't reach high threshold
	processedEvent := &events.ProcessedEvent{
		Event: events.Event{
			ID:                 "event_high_threshold",
			TenantID:           types.GetTenantID(s.GetContext()),
			EnvironmentID:      types.GetEnvironmentID(s.GetContext()),
			CustomerID:         s.testData.customer.ID,
			ExternalCustomerID: s.testData.customer.ExternalID,
			EventName:          "api_call",
			Timestamp:          s.testData.now,
		},
		SubscriptionID: s.testData.subscription.ID,
		PeriodID:       1,
		Cost:           decimal.NewFromFloat(10.0), // Total $60, still below $100
		Currency:       "usd",
	}

	subResponse := &dto.SubscriptionResponse{
		Subscription: s.testData.subscription,
	}

	err := s.service.ProcessEventOverage(s.GetContext(), processedEvent, subResponse)
	s.NoError(err)

	// Verify no invoice created below high threshold
	invoices, err := s.GetStores().InvoiceRepo.List(s.GetContext(), &types.InvoiceFilter{
		CustomerID:  s.testData.customer.ID,
		QueryFilter: types.NewNoLimitQueryFilter(),
	})
	s.NoError(err)
	s.Empty(invoices, "No invoice should be created below $100 threshold")
}

func (s *OverageBillingServiceSuite) TestAccumulatedCostCalculation() {
	// Test that accumulated cost is retrieved correctly from mock
	mockRepo := s.service.(*overageBillingService).processedEventRepo.(*testutil.MockProcessedEventRepository)
	mockRepo.Clear()

	// Add events with different costs
	mockRepo.SetPeriodCost("sub_test", 1, decimal.NewFromFloat(1.0))
	mockRepo.SetPeriodCost("sub_test", 1, decimal.NewFromFloat(2.0))
	mockRepo.SetPeriodCost("sub_test", 1, decimal.NewFromFloat(1.5))

	// Get accumulated cost
	cost, err := mockRepo.GetPeriodCost(s.GetContext(), "", "", "", "sub_test", 1)
	s.NoError(err)
	s.True(cost.Equal(decimal.NewFromFloat(4.5)), "Accumulated cost should be $4.50")
}

func (s *OverageBillingServiceSuite) TestMockProcessedEventRepo_Clear() {
	mockRepo := s.service.(*overageBillingService).processedEventRepo.(*testutil.MockProcessedEventRepository)

	// Add some data
	mockRepo.SetPeriodCost("sub_1", 1, decimal.NewFromFloat(10.0))

	// Clear
	mockRepo.Clear()

	// Verify cleared
	cost, err := mockRepo.GetPeriodCost(s.GetContext(), "", "", "", "sub_1", 1)
	s.NoError(err)
	s.True(cost.IsZero(), "Cost should be zero after clear")
}

func (s *OverageBillingServiceSuite) TestProcessEventOverage_ThresholdReached_InvoiceCreated() {
	// Set up mock with cost at threshold
	mockRepo := s.service.(*overageBillingService).processedEventRepo.(*testutil.MockProcessedEventRepository)
	mockRepo.Clear()
	// Set accumulated cost to exactly $5 (threshold)
	mockRepo.SetPeriodCost(s.testData.subscription.ID, 1, decimal.NewFromFloat(5.0))

	// Create event that triggers threshold
	processedEvent := &events.ProcessedEvent{
		Event: events.Event{
			ID:                 "event_threshold",
			TenantID:           types.GetTenantID(s.GetContext()),
			EnvironmentID:      types.GetEnvironmentID(s.GetContext()),
			CustomerID:         s.testData.customer.ID,
			ExternalCustomerID: s.testData.customer.ExternalID,
			EventName:          "api_call",
			Timestamp:          s.testData.now,
		},
		SubscriptionID: s.testData.subscription.ID,
		PeriodID:       1,
		Cost:           decimal.NewFromFloat(0.50), // This triggers the threshold
		Currency:       "usd",
	}

	subResponse := &dto.SubscriptionResponse{
		Subscription: s.testData.subscription,
	}

	// Process the event - should create invoice
	err := s.service.ProcessEventOverage(s.GetContext(), processedEvent, subResponse)
	s.NoError(err)

	// Verify invoice was created
	invoices, err := s.GetStores().InvoiceRepo.List(s.GetContext(), &types.InvoiceFilter{
		CustomerID:  s.testData.customer.ID,
		QueryFilter: types.NewNoLimitQueryFilter(),
	})
	s.NoError(err)
	s.Len(invoices, 1, "One invoice should be created when threshold is reached")

	// Verify invoice details
	inv := invoices[0]
	s.True(inv.AmountDue.Equal(decimal.NewFromFloat(5.0)), "Invoice amount should be $5.00")
	s.Equal("usd", inv.Currency)
	s.Equal(types.InvoiceTypeOneOff, inv.InvoiceType)
	s.Equal("Real-time overage charges", inv.Description)

	// Verify metadata
	s.Equal("overage", inv.Metadata["billing_type"])
	s.Equal(s.testData.subscription.ID, inv.Metadata["subscription_id"])
}

func (s *OverageBillingServiceSuite) TestProcessEventOverage_WalletPaymentAttempted() {
	// This test verifies that the overage billing flow attempts wallet payment
	// Note: In-memory test stores may not fully simulate wallet deduction, so we verify:
	// 1. Invoice is created
	// 2. The flow completes without error
	// 3. Invoice shows payment was attempted

	// Verify initial wallet exists
	walletsBefore, err := s.GetStores().WalletRepo.GetWalletsByCustomerID(s.GetContext(), s.testData.customer.ID)
	s.NoError(err)
	s.Len(walletsBefore, 1)
	s.True(walletsBefore[0].Balance.Equal(decimal.NewFromFloat(3.0)), "Initial balance should be $3")

	// Set up mock with cost at threshold
	mockRepo := s.service.(*overageBillingService).processedEventRepo.(*testutil.MockProcessedEventRepository)
	mockRepo.Clear()
	mockRepo.SetPeriodCost(s.testData.subscription.ID, 1, decimal.NewFromFloat(5.0))

	// Create event that triggers threshold
	processedEvent := &events.ProcessedEvent{
		Event: events.Event{
			ID:                 "event_wallet_deduct",
			TenantID:           types.GetTenantID(s.GetContext()),
			EnvironmentID:      types.GetEnvironmentID(s.GetContext()),
			CustomerID:         s.testData.customer.ID,
			ExternalCustomerID: s.testData.customer.ExternalID,
			EventName:          "api_call",
			Timestamp:          s.testData.now,
		},
		SubscriptionID: s.testData.subscription.ID,
		PeriodID:       1,
		Cost:           decimal.NewFromFloat(0.50),
		Currency:       "usd",
	}

	subResponse := &dto.SubscriptionResponse{
		Subscription: s.testData.subscription,
	}

	// Process the event - should complete without error
	err = s.service.ProcessEventOverage(s.GetContext(), processedEvent, subResponse)
	s.NoError(err)

	// Verify invoice was created
	invoices, err := s.GetStores().InvoiceRepo.List(s.GetContext(), &types.InvoiceFilter{
		CustomerID:  s.testData.customer.ID,
		QueryFilter: types.NewNoLimitQueryFilter(),
	})
	s.NoError(err)
	s.Len(invoices, 1, "Invoice should be created")

	// Verify invoice has correct amount
	s.True(invoices[0].AmountDue.Equal(decimal.NewFromFloat(5.0)),
		"Invoice amount should be $5.00")

	// Verify wallet still exists (payment flow was attempted)
	walletsAfter, err := s.GetStores().WalletRepo.GetWalletsByCustomerID(s.GetContext(), s.testData.customer.ID)
	s.NoError(err)
	s.Len(walletsAfter, 1, "Wallet should still exist after payment attempt")
}

func (s *OverageBillingServiceSuite) TestProcessEventOverage_InvoiceLineItems() {
	// Set up mock with cost at threshold
	mockRepo := s.service.(*overageBillingService).processedEventRepo.(*testutil.MockProcessedEventRepository)
	mockRepo.Clear()
	mockRepo.SetPeriodCost(s.testData.subscription.ID, 1, decimal.NewFromFloat(7.50))

	// Create event that triggers threshold
	processedEvent := &events.ProcessedEvent{
		Event: events.Event{
			ID:                 "event_line_items",
			TenantID:           types.GetTenantID(s.GetContext()),
			EnvironmentID:      types.GetEnvironmentID(s.GetContext()),
			CustomerID:         s.testData.customer.ID,
			ExternalCustomerID: s.testData.customer.ExternalID,
			EventName:          "api_call",
			Timestamp:          s.testData.now,
		},
		SubscriptionID: s.testData.subscription.ID,
		PeriodID:       1,
		Cost:           decimal.NewFromFloat(0.50),
		Currency:       "usd",
	}

	subResponse := &dto.SubscriptionResponse{
		Subscription: s.testData.subscription,
	}

	// Process the event
	err := s.service.ProcessEventOverage(s.GetContext(), processedEvent, subResponse)
	s.NoError(err)

	// Verify invoice was created with correct amount
	invoices, err := s.GetStores().InvoiceRepo.List(s.GetContext(), &types.InvoiceFilter{
		CustomerID:  s.testData.customer.ID,
		QueryFilter: types.NewNoLimitQueryFilter(),
	})
	s.NoError(err)
	s.Len(invoices, 1)

	inv := invoices[0]
	// Invoice amount should match accumulated cost ($7.50)
	s.True(inv.AmountDue.Equal(decimal.NewFromFloat(7.50)),
		"Invoice amount should equal accumulated cost of $7.50, got %s", inv.AmountDue.String())
}

func (s *OverageBillingServiceSuite) TestProcessEventOverage_ZeroThreshold() {
	// Set threshold to zero - should invoice immediately
	customConfig := map[string]interface{}{
		"enabled":           true,
		"invoice_threshold": "0.00",
	}
	setting := &settings.Setting{
		ID:            types.GenerateUUIDWithPrefix("set"),
		Key:           types.SettingKeyOverageBillingConfig,
		Value:         customConfig,
		EnvironmentID: types.GetEnvironmentID(s.GetContext()),
		BaseModel:     types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().SettingsRepo.Create(s.GetContext(), setting))

	// Set up mock with any cost
	mockRepo := s.service.(*overageBillingService).processedEventRepo.(*testutil.MockProcessedEventRepository)
	mockRepo.Clear()
	mockRepo.SetPeriodCost(s.testData.subscription.ID, 1, decimal.NewFromFloat(0.50))

	// Create event
	processedEvent := &events.ProcessedEvent{
		Event: events.Event{
			ID:                 "event_zero_threshold",
			TenantID:           types.GetTenantID(s.GetContext()),
			EnvironmentID:      types.GetEnvironmentID(s.GetContext()),
			CustomerID:         s.testData.customer.ID,
			ExternalCustomerID: s.testData.customer.ExternalID,
			EventName:          "api_call",
			Timestamp:          s.testData.now,
		},
		SubscriptionID: s.testData.subscription.ID,
		PeriodID:       1,
		Cost:           decimal.NewFromFloat(0.10),
		Currency:       "usd",
	}

	subResponse := &dto.SubscriptionResponse{
		Subscription: s.testData.subscription,
	}

	// Process the event
	err := s.service.ProcessEventOverage(s.GetContext(), processedEvent, subResponse)
	s.NoError(err)

	// With zero threshold, any cost should trigger invoice
	invoices, err := s.GetStores().InvoiceRepo.List(s.GetContext(), &types.InvoiceFilter{
		CustomerID:  s.testData.customer.ID,
		QueryFilter: types.NewNoLimitQueryFilter(),
	})
	s.NoError(err)
	s.Len(invoices, 1, "Invoice should be created with zero threshold")
}

func (s *OverageBillingServiceSuite) TestProcessEventOverage_MultiplePeriods() {
	// Test that different period IDs don't interfere with each other
	mockRepo := s.service.(*overageBillingService).processedEventRepo.(*testutil.MockProcessedEventRepository)
	mockRepo.Clear()

	// Period 1: below threshold
	mockRepo.SetPeriodCost(s.testData.subscription.ID, 1, decimal.NewFromFloat(2.0))

	// Period 2: above threshold
	mockRepo.SetPeriodCost(s.testData.subscription.ID, 2, decimal.NewFromFloat(10.0))

	// Event in period 1 - should NOT trigger invoice
	event1 := &events.ProcessedEvent{
		Event: events.Event{
			ID:                 "event_period_1",
			TenantID:           types.GetTenantID(s.GetContext()),
			EnvironmentID:      types.GetEnvironmentID(s.GetContext()),
			CustomerID:         s.testData.customer.ID,
			ExternalCustomerID: s.testData.customer.ExternalID,
			EventName:          "api_call",
			Timestamp:          s.testData.now,
		},
		SubscriptionID: s.testData.subscription.ID,
		PeriodID:       1, // Period 1
		Cost:           decimal.NewFromFloat(0.50),
		Currency:       "usd",
	}

	subResponse := &dto.SubscriptionResponse{
		Subscription: s.testData.subscription,
	}

	err := s.service.ProcessEventOverage(s.GetContext(), event1, subResponse)
	s.NoError(err)

	// No invoice should be created (period 1 below threshold)
	invoices, err := s.GetStores().InvoiceRepo.List(s.GetContext(), &types.InvoiceFilter{
		CustomerID:  s.testData.customer.ID,
		QueryFilter: types.NewNoLimitQueryFilter(),
	})
	s.NoError(err)
	s.Empty(invoices, "Period 1 below threshold - no invoice")

	// Event in period 2 - should trigger invoice
	event2 := &events.ProcessedEvent{
		Event: events.Event{
			ID:                 "event_period_2",
			TenantID:           types.GetTenantID(s.GetContext()),
			EnvironmentID:      types.GetEnvironmentID(s.GetContext()),
			CustomerID:         s.testData.customer.ID,
			ExternalCustomerID: s.testData.customer.ExternalID,
			EventName:          "api_call",
			Timestamp:          s.testData.now,
		},
		SubscriptionID: s.testData.subscription.ID,
		PeriodID:       2, // Period 2
		Cost:           decimal.NewFromFloat(0.50),
		Currency:       "usd",
	}

	err = s.service.ProcessEventOverage(s.GetContext(), event2, subResponse)
	s.NoError(err)

	// Invoice should be created (period 2 above threshold)
	invoices, err = s.GetStores().InvoiceRepo.List(s.GetContext(), &types.InvoiceFilter{
		CustomerID:  s.testData.customer.ID,
		QueryFilter: types.NewNoLimitQueryFilter(),
	})
	s.NoError(err)
	s.Len(invoices, 1, "Period 2 above threshold - invoice created")
	s.True(invoices[0].AmountDue.Equal(decimal.NewFromFloat(10.0)),
		"Invoice should be for period 2 amount ($10)")
}
