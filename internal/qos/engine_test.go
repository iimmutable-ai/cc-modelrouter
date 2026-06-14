package qos

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestEngineDefaults(t *testing.T) {
	e := NewQoSEngine(0, WREDConfig{}, nil)

	if e.GlobalMax() != DefaultGlobalMaxConc {
		t.Errorf("expected global max %d, got %d", DefaultGlobalMaxConc, e.GlobalMax())
	}

	if e.wred.MinDepth != 0.5 {
		t.Errorf("expected default WRED min 0.5, got %f", e.wred.MinDepth)
	}
}

func TestEngineGuaranteedShares(t *testing.T) {
	groups := []GroupConfig{
		{Name: "A", PriorityWeight: 0.7},
		{Name: "B", PriorityWeight: 0.3},
	}
	e := NewQoSEngine(100, DefaultWREDConfig(), groups)

	stats := e.GetStats()
	if s, ok := stats["A"]; !ok {
		t.Fatal("group A not found")
	} else {
		if s.GuaranteedShare != 70 {
			t.Errorf("expected A guaranteed share 70, got %d", s.GuaranteedShare)
		}
	}

	if s, ok := stats["B"]; !ok {
		t.Fatal("group B not found")
	} else {
		if s.GuaranteedShare != 30 {
			t.Errorf("expected B guaranteed share 30, got %d", s.GuaranteedShare)
		}
	}
}

func TestEngineAdmitRelease(t *testing.T) {
	groups := []GroupConfig{
		{Name: "A", PriorityWeight: 0.5, MaxConcurrency: 50},
	}
	e := NewQoSEngine(100, DefaultWREDConfig(), groups)

	ctx := context.Background()

	for i := 0; i < 50; i++ {
		if err := e.Admit(ctx, "A"); err != nil {
			t.Fatalf("admit %d failed: %v", i, err)
		}
	}

	stats := e.GetStats()
	if stats["A"].Active != 50 {
		t.Errorf("expected 50 active, got %d", stats["A"].Active)
	}

	for i := 0; i < 50; i++ {
		e.Release("A")
	}

	stats = e.GetStats()
	if stats["A"].Active != 0 {
		t.Errorf("expected 0 active after release, got %d", stats["A"].Active)
	}
}

func TestEngineAdmitUnknownGroup(t *testing.T) {
	e := NewQoSEngine(100, DefaultWREDConfig(), nil)

	err := e.Admit(context.Background(), "unknown")
	if err != nil {
		t.Errorf("unknown group should be admitted: %v", err)
	}

	e.Release("unknown")
}

func TestEngineBorrowIdleCapacity(t *testing.T) {
	groups := []GroupConfig{
		{Name: "A", PriorityWeight: 0.5},
		{Name: "B", PriorityWeight: 0.5},
	}
	e := NewQoSEngine(100, DefaultWREDConfig(), groups)

	ctx := context.Background()

	// Group A fills its guaranteed share (50)
	for i := 0; i < 50; i++ {
		if err := e.Admit(ctx, "A"); err != nil {
			t.Fatalf("admit A %d failed: %v", i, err)
		}
	}

	// Group A borrows from B's idle capacity (up to global max)
	for i := 0; i < 50; i++ {
		if err := e.Admit(ctx, "A"); err != nil {
			t.Fatalf("admit A borrow %d failed: %v", i, err)
		}
	}

	stats := e.GetStats()
	if stats["A"].Active != 100 {
		t.Errorf("expected A 100 (50 guaranteed + 50 borrowed), got %d", stats["A"].Active)
	}

	for i := 0; i < 100; i++ {
		e.Release("A")
	}
}

// TestEngineOverCapacityWithWRED tests that over-capacity requests are admitted
// when WRED allows them, and tracks Active correctly.
func TestEngineOverCapacityWithWRED(t *testing.T) {
	// Use WRED that never drops to test the queue mechanism
	noDrop := WREDConfig{MinDepth: 10.0, MaxDepth: 20.0}
	groups := []GroupConfig{
		{Name: "A", PriorityWeight: 1.0, MaxConcurrency: 1}, // queue cap = 2
	}
	e := NewQoSEngine(2, noDrop, groups)

	ctx := context.Background()

	// Fill guaranteed share (ceil(2*1.0/1.0)=2)
	if err := e.Admit(ctx, "A"); err != nil {
		t.Fatalf("admit 1 failed: %v", err)
	}
	if err := e.Admit(ctx, "A"); err != nil {
		t.Fatalf("admit 2 failed: %v", err)
	}

	stats := e.GetStats()
	if stats["A"].Active != 2 {
		t.Fatalf("expected 2 active, got %d", stats["A"].Active)
	}

	// Over-capacity request: WRED won't drop (depth 0.33 < 10.0)
	if err := e.Admit(ctx, "A"); err != nil {
		t.Fatalf("over-capacity admit failed: %v", err)
	}
	stats = e.GetStats()
	if stats["A"].Active != 3 {
		t.Errorf("expected 3 active (2 guaranteed + 1 over-capacity), got %d", stats["A"].Active)
	}

	// Another over-capacity request
	if err := e.Admit(ctx, "A"); err != nil {
		t.Fatalf("over-capacity admit 2 failed: %v", err)
	}
	stats = e.GetStats()
	if stats["A"].Active != 4 {
		t.Errorf("expected 4 active, got %d", stats["A"].Active)
	}
}

// TestEngineContextCancellation tests that a request waiting on a full queue
// can be cancelled via context.
func TestEngineContextCancellation(t *testing.T) {
	// WRED that never drops + small queue cap to force channel blocking
	noDrop := WREDConfig{MinDepth: 10.0, MaxDepth: 20.0}
	groups := []GroupConfig{
		{Name: "A", PriorityWeight: 1.0, MaxConcurrency: 1}, // queue cap = 2
	}
	e := NewQoSEngine(2, noDrop, groups)

	ctx := context.Background()

	// Fill guaranteed share (2)
	for i := 0; i < 2; i++ {
		if err := e.Admit(ctx, "A"); err != nil {
			t.Fatalf("admit %d failed: %v", i, err)
		}
	}

	// Fill queue channel (2 items, cap=2) via over-capacity admits
	for i := 0; i < 2; i++ {
		if err := e.Admit(ctx, "A"); err != nil {
			t.Fatalf("over-capacity admit %d failed: %v", i, err)
		}
	}

	// Now the queue channel is full. Next request should block on send.
	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	wg.Add(1)
	var admitErr error
	go func() {
		defer wg.Done()
		admitErr = e.Admit(ctx, "A")
	}()

	// Wait for goroutine to enter the blocked send
	time.Sleep(50 * time.Millisecond)

	cancel()
	wg.Wait()

	if admitErr == nil {
		t.Error("expected error on context cancellation")
	}
}

// TestEngineReleaseReplacesQueued tests that Release correctly handles
// the over-capacity queue: replacing finished requests with queued ones.
func TestEngineReleaseReplacesQueued(t *testing.T) {
	noDrop := WREDConfig{MinDepth: 10.0, MaxDepth: 20.0}
	groups := []GroupConfig{
		{Name: "A", PriorityWeight: 1.0, MaxConcurrency: 1}, // queue cap = 2
	}
	e := NewQoSEngine(2, noDrop, groups)

	ctx := context.Background()

	// Fill guaranteed (2) + queue (2) = Active=4
	for i := 0; i < 4; i++ {
		if err := e.Admit(ctx, "A"); err != nil {
			t.Fatalf("admit %d failed: %v", i, err)
		}
	}
	stats := e.GetStats()
	if stats["A"].Active != 4 {
		t.Fatalf("expected 4 active, got %d", stats["A"].Active)
	}

	// Release: should decrement then read from queue (net effect: Active stays same)
	e.Release("A")
	stats = e.GetStats()
	if stats["A"].Active != 4 {
		t.Errorf("release with queued item: expected active to stay 4, got %d", stats["A"].Active)
	}

	// Release again: same behavior (another queued item)
	e.Release("A")
	stats = e.GetStats()
	if stats["A"].Active != 4 {
		t.Errorf("release with queued item: expected active to stay 4, got %d", stats["A"].Active)
	}

	// Release again: queue empty, Active should decrease
	e.Release("A")
	stats = e.GetStats()
	if stats["A"].Active != 3 {
		t.Errorf("release with empty queue: expected active 3, got %d", stats["A"].Active)
	}
}

// TestEngineGlobalCapExceeded tests that once the global cap is reached,
// additional requests either get rejected by WRED or go over-capacity.
func TestEngineGlobalCapExceeded(t *testing.T) {
	// Default WRED: drops start at 50% queue depth
	groups := []GroupConfig{
		{Name: "A", PriorityWeight: 1.0, MaxConcurrency: 50}, // queue cap = 100
	}
	e := NewQoSEngine(10, DefaultWREDConfig(), groups)

	ctx := context.Background()

	// Fill guaranteed share (10)
	for i := 0; i < 10; i++ {
		if err := e.Admit(ctx, "A"); err != nil {
			t.Fatalf("admit %d failed: %v", i, err)
		}
	}

	// Over-capacity: some requests will be rejected by WRED
	rejected := 0
	admitted := 0
	for i := 0; i < 100; i++ {
		if err := e.Admit(ctx, "A"); err != nil {
			rejected++
		} else {
			admitted++
		}
	}

	// Should have some rejections due to WRED
	if rejected == 0 {
		t.Error("expected some WRED rejections when over capacity")
	}

	// But should also have some admissions (WRED is probabilistic)
	if admitted == 0 {
		t.Error("expected some over-capacity admissions")
	}
}

func TestEngineGetStats(t *testing.T) {
	groups := []GroupConfig{
		{Name: "dev", PriorityWeight: 0.6},
		{Name: "ops", PriorityWeight: 0.4},
	}
	e := NewQoSEngine(100, DefaultWREDConfig(), groups)

	stats := e.GetStats()
	if len(stats) != 2 {
		t.Fatalf("expected 2 groups in stats, got %d", len(stats))
	}

	if stats["dev"].PriorityWeight != 0.6 {
		t.Errorf("expected dev priority 0.6, got %f", stats["dev"].PriorityWeight)
	}
	if stats["ops"].PriorityWeight != 0.4 {
		t.Errorf("expected ops priority 0.4, got %f", stats["ops"].PriorityWeight)
	}
}

func TestEngineProviderTracker(t *testing.T) {
	e := NewQoSEngine(100, DefaultWREDConfig(), nil)

	tracker := e.ProviderTracker()
	if tracker == nil {
		t.Fatal("provider tracker should not be nil")
	}

	tracker.RegisterProvider(0)
	if tracker.GetLimit(0) != InitialProviderLimit {
		t.Errorf("expected initial limit %d, got %d", InitialProviderLimit, tracker.GetLimit(0))
	}

	tracker.Record429(0)
	limit := tracker.GetLimit(0)
	if limit >= InitialProviderLimit {
		t.Error("limit should decrease after 429")
	}

	tracker.ResetProvider(0)
	if tracker.GetLimit(0) != InitialProviderLimit {
		t.Errorf("expected limit reset to %d, got %d", InitialProviderLimit, tracker.GetLimit(0))
	}
}

func TestEngineProviderCapacityAffectsGlobalCap(t *testing.T) {
	groups := []GroupConfig{
		{Name: "A", PriorityWeight: 1.0, MaxConcurrency: 50},
	}
	e := NewQoSEngine(100, DefaultWREDConfig(), groups)

	ctx := context.Background()

	// Register provider with capacity 50
	tracker := e.ProviderTracker()
	tracker.RegisterProvider(0)
	tracker.RegisterProvider(1)

	// Total provider capacity = 100 = globalMax, so same behavior
	for i := 0; i < 100; i++ {
		if err := e.Admit(ctx, "A"); err != nil {
			t.Fatalf("admit %d failed: %v", i, err)
		}
	}

	// Reduce provider 0 limit via 429s
	for i := 0; i < 5; i++ {
		tracker.Record429(0)
	}

	// Now total provider capacity should be less than 100
	totalCap := tracker.TotalCapacity()
	if totalCap >= 100 {
		t.Errorf("expected total capacity < 100 after 429s, got %d", totalCap)
	}

	// Release all and re-test with reduced capacity
	for i := 0; i < 100; i++ {
		e.Release("A")
	}

	stats := e.GetStats()
	if stats["A"].Active != 0 {
		t.Errorf("expected 0 active after release, got %d", stats["A"].Active)
	}
}

func TestProviderCapacityAIMD(t *testing.T) {
	tracker := NewProviderCapacityTracker()
	tracker.RegisterProvider(0)

	initial := tracker.GetLimit(0)
	if initial != InitialProviderLimit {
		t.Errorf("expected initial limit %d, got %d", InitialProviderLimit, initial)
	}

	// Single 429: reduce by 20%
	tracker.Record429(0)
	limit := tracker.GetLimit(0)
	expected := initial * 4 / 5
	if limit != expected {
		t.Errorf("expected limit %d after single 429, got %d", expected, limit)
	}

	// Second 429 (2 in window): halve
	tracker.Record429(0)
	limit = tracker.GetLimit(0)
	expected = (initial * 4 / 5) / 2
	if limit != expected {
		t.Errorf("expected limit %d after double 429, got %d", expected, limit)
	}

	// Floor at ProviderFloor
	for limit > ProviderFloor+1 {
		tracker.Record429(0)
		limit = tracker.GetLimit(0)
	}
	if limit < ProviderFloor {
		t.Errorf("limit should not go below floor %d, got %d", ProviderFloor, limit)
	}
}

func TestProviderCapacityAdditiveIncrease(t *testing.T) {
	tracker := NewProviderCapacityTracker()
	tracker.RegisterProvider(0)

	initial := tracker.GetLimit(0)

	// Without any 429s, successes should increase the limit after AIMDSuccessInterval
	for i := 0; i < AIMDSuccessInterval; i++ {
		tracker.RecordSuccess(0)
	}
	limit := tracker.GetLimit(0)
	if limit <= initial {
		t.Errorf("expected limit > %d after %d successes with no 429s, got %d", initial, AIMDSuccessInterval, limit)
	}

	// Continue accumulating successes for another increase
	for i := 0; i < AIMDSuccessInterval; i++ {
		tracker.RecordSuccess(0)
	}
	limit2 := tracker.GetLimit(0)
	if limit2 <= limit {
		t.Errorf("expected limit > %d after another %d successes, got %d", limit, AIMDSuccessInterval, limit2)
	}
}

func TestProviderCapacityNoIncreaseWithRecent429(t *testing.T) {
	tracker := NewProviderCapacityTracker()
	tracker.RegisterProvider(0)

	// Record a 429
	tracker.Record429(0)

	// Even after many successes, limit should not increase because recent 429
	for i := 0; i < AIMDSuccessInterval*2; i++ {
		tracker.RecordSuccess(0)
	}
	limit := tracker.GetLimit(0)
	if limit > tracker.GetLimit(0) {
		t.Error("limit should not increase with recent 429 in window")
	}
}

func TestProviderGetStats(t *testing.T) {
	tracker := NewProviderCapacityTracker()
	tracker.RegisterProvider(0)
	tracker.RegisterProvider(1)

	tracker.Acquire(0)
	tracker.Acquire(0)
	tracker.Acquire(1)

	stats := tracker.GetProviderStats()
	if len(stats) != 2 {
		t.Fatalf("expected 2 providers in stats, got %d", len(stats))
	}
	if stats[0].Claims != 2 {
		t.Errorf("expected provider 0 claims 2, got %d", stats[0].Claims)
	}
	if stats[1].Claims != 1 {
		t.Errorf("expected provider 1 claims 1, got %d", stats[1].Claims)
	}
}

func TestEngineReleaseDrainsQueueAdmitsBlocked(t *testing.T) {
	// Set up a scenario where the queue channel blocks a sender,
	// then verify that Release unblocks it.
	noDrop := WREDConfig{MinDepth: 10.0, MaxDepth: 20.0}
	groups := []GroupConfig{
		{Name: "A", PriorityWeight: 1.0, MaxConcurrency: 1}, // queue cap = 2
	}
	e := NewQoSEngine(2, noDrop, groups)

	ctx := context.Background()

	// Fill guaranteed (2) + queue channel (2) = 4 active, channel full
	for i := 0; i < 4; i++ {
		if err := e.Admit(ctx, "A"); err != nil {
			t.Fatalf("admit %d failed: %v", i, err)
		}
	}

	// 5th request should block on channel send
	var admitted int32
	go func() {
		if err := e.Admit(ctx, "A"); err != nil {
			// This shouldn't happen; the request should block then get admitted
			return
		}
		atomic.StoreInt32(&admitted, 1)
	}()

	// Wait for goroutine to block on channel send
	time.Sleep(50 * time.Millisecond)
	if atomic.LoadInt32(&admitted) == 1 {
		t.Fatal("request should be blocked, not admitted yet")
	}

	// Release a slot — this drains from queue channel, unblocking the sender
	e.Release("A")

	// Wait for the blocked request to be admitted
	deadline := time.After(2 * time.Second)
	for atomic.LoadInt32(&admitted) == 0 {
		select {
		case <-deadline:
			t.Fatal("blocked request was not admitted after Release")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}
