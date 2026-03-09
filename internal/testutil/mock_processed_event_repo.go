package testutil

import (
	"context"
	"sync"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/shopspring/decimal"
)

// MockProcessedEventRepository is a mock implementation of events.ProcessedEventRepository
type MockProcessedEventRepository struct {
	mu             sync.RWMutex
	processedEvents []*events.ProcessedEvent
	periodCosts    map[string]decimal.Decimal // key: subscriptionID-periodID
}

// NewMockProcessedEventRepository creates a new mock processed event repository
func NewMockProcessedEventRepository() *MockProcessedEventRepository {
	return &MockProcessedEventRepository{
		processedEvents: make([]*events.ProcessedEvent, 0),
		periodCosts:    make(map[string]decimal.Decimal),
	}
}

// InsertProcessedEvent inserts a single processed event
func (r *MockProcessedEventRepository) InsertProcessedEvent(ctx context.Context, event *events.ProcessedEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.processedEvents = append(r.processedEvents, event)
	return nil
}

// BulkInsertProcessedEvents bulk inserts processed events
func (r *MockProcessedEventRepository) BulkInsertProcessedEvents(ctx context.Context, evts []*events.ProcessedEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.processedEvents = append(r.processedEvents, evts...)
	return nil
}

// GetProcessedEvents gets processed events with filtering
func (r *MockProcessedEventRepository) GetProcessedEvents(ctx context.Context, params *events.GetProcessedEventsParams) ([]*events.ProcessedEvent, uint64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.processedEvents, uint64(len(r.processedEvents)), nil
}

// IsDuplicate checks for duplicate events
func (r *MockProcessedEventRepository) IsDuplicate(ctx context.Context, subscriptionID, meterID string, periodID uint64, uniqueHash string) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, e := range r.processedEvents {
		if e.SubscriptionID == subscriptionID && e.MeterID == meterID && e.PeriodID == periodID && e.UniqueHash == uniqueHash {
			return true, nil
		}
	}
	return false, nil
}

// GetLineItemUsage gets usage for a line item
func (r *MockProcessedEventRepository) GetLineItemUsage(ctx context.Context, subLineItemID string, periodID uint64) (decimal.Decimal, decimal.Decimal, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	totalQty := decimal.Zero
	freeUnits := decimal.Zero
	for _, e := range r.processedEvents {
		if e.SubLineItemID == subLineItemID && e.PeriodID == periodID {
			totalQty = totalQty.Add(e.QtyBillable)
			freeUnits = freeUnits.Add(e.QtyFreeApplied)
		}
	}
	return totalQty, freeUnits, nil
}

// GetPeriodCost gets the accumulated cost for a subscription period
func (r *MockProcessedEventRepository) GetPeriodCost(ctx context.Context, tenantID, environmentID, customerID, subscriptionID string, periodID uint64) (decimal.Decimal, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Sum up all costs for this subscription and period
	totalCost := decimal.Zero
	for _, e := range r.processedEvents {
		if e.SubscriptionID == subscriptionID && e.PeriodID == periodID {
			totalCost = totalCost.Add(e.Cost)
		}
	}
	return totalCost, nil
}

// GetPeriodFeatureTotals gets feature totals for a period
func (r *MockProcessedEventRepository) GetPeriodFeatureTotals(ctx context.Context, tenantID, environmentID, customerID, subscriptionID string, periodID uint64) ([]*events.PeriodFeatureTotal, error) {
	return nil, nil
}

// GetUsageAnalytics gets usage analytics
func (r *MockProcessedEventRepository) GetUsageAnalytics(ctx context.Context, tenantID, environmentID, customerID string, lookbackHours int) ([]*events.UsageAnalytic, error) {
	return nil, nil
}

// GetDetailedUsageAnalytics gets detailed usage analytics
func (r *MockProcessedEventRepository) GetDetailedUsageAnalytics(ctx context.Context, params *events.UsageAnalyticsParams) ([]*events.DetailedUsageAnalytic, error) {
	return nil, nil
}

// SetPeriodCost allows tests to set a predefined period cost
func (r *MockProcessedEventRepository) SetPeriodCost(subscriptionID string, periodID uint64, cost decimal.Decimal) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Add a mock event with this cost
	r.processedEvents = append(r.processedEvents, &events.ProcessedEvent{
		SubscriptionID: subscriptionID,
		PeriodID:       periodID,
		Cost:           cost,
	})
}

// Clear clears all stored events
func (r *MockProcessedEventRepository) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.processedEvents = make([]*events.ProcessedEvent, 0)
	r.periodCosts = make(map[string]decimal.Decimal)
}

// GetStoredEvents returns all stored events for testing
func (r *MockProcessedEventRepository) GetStoredEvents() []*events.ProcessedEvent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*events.ProcessedEvent, len(r.processedEvents))
	copy(result, r.processedEvents)
	return result
}
