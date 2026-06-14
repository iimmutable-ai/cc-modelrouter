package usage

import (
	"database/sql"
	"sync"
	"time"

	"github.com/iimmutable/cc-modelrouter/internal/logging"
)

const (
	DefaultBufferSize    = 500
	DefaultFlushTimeout = 1 * time.Second // Reduced from 3s for near-realtime monitor updates
	channelCapacity     = 1000
)

// Tracker tracks usage records in memory and flushes to database.
// Uses a buffered channel for lock-free submission on the hot path,
// with a single goroutine handling batching and SQLite writes.
type Tracker struct {
	db           *sql.DB
	recordCh     chan *Record
	bufferSize   int
	flushTimeout time.Duration
	done         chan struct{}
	wg           sync.WaitGroup
}

// NewTracker creates a new usage tracker.
func NewTracker(db *sql.DB, bufferSize int, flushTimeout time.Duration) *Tracker {
	if bufferSize <= 0 {
		bufferSize = DefaultBufferSize
	}
	if flushTimeout <= 0 {
		flushTimeout = DefaultFlushTimeout
	}

	t := &Tracker{
		db:           db,
		recordCh:     make(chan *Record, channelCapacity),
		bufferSize:   bufferSize,
		flushTimeout: flushTimeout,
		done:         make(chan struct{}),
	}

	t.wg.Add(1)
	go t.flushLoop()

	return t
}

// Record adds a usage record via a buffered channel.
// Zero mutex contention on the hot path — the single flushLoop goroutine
// handles all SQLite writes.
func (t *Tracker) Record(instanceID, route, model, profile, provider string, tokens, fallbacks int) {
	t.recordCh <- &Record{
		InstanceID: instanceID,
		Profile:    profile,
		Provider:   provider,
		Route:      route,
		Model:      model,
		Tokens:     tokens,
		Fallbacks:  fallbacks,
		Timestamp:  time.Now(),
	}
}

// RecordWithUser adds a usage record with multi-user context via a buffered channel.
func (t *Tracker) RecordWithUser(instanceID, route, model, profile, provider string, tokens, fallbacks int, keyPrefix, groupName string) {
	t.recordCh <- &Record{
		InstanceID:   instanceID,
		Profile:      profile,
		Provider:     provider,
		Route:        route,
		Model:        model,
		Tokens:       tokens,
		Fallbacks:    fallbacks,
		Timestamp:    time.Now(),
		APIKeyPrefix: keyPrefix,
		GroupName:    groupName,
	}
}

// flush writes buffered records to database.
// Called only from the flushLoop goroutine — no mutex needed.
func (t *Tracker) flush(records []*Record) {
	if len(records) == 0 {
		return
	}

	tx, err := t.db.Begin()
	if err != nil {
		logging.Errorf("usage tracker: failed to begin transaction: %v", err)
		return
	}
	defer tx.Rollback()

	for i, r := range records {
		if err := InsertRecord(tx, r); err != nil {
			logging.Errorf("usage tracker: failed to insert record %d: %v", i, err)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		logging.Errorf("usage tracker: failed to commit transaction: %v", err)
		return
	}
}

// flushLoop receives records from the channel, batches them, and flushes
// to SQLite periodically or when the batch reaches bufferSize.
func (t *Tracker) flushLoop() {
	defer t.wg.Done()

	buffer := make([]*Record, 0, t.bufferSize)
	timer := time.NewTimer(t.flushTimeout)
	defer timer.Stop()

	for {
		select {
		case record := <-t.recordCh:
			buffer = append(buffer, record)
			if len(buffer) >= t.bufferSize {
				t.flush(buffer)
				buffer = buffer[:0]
				// Reset timer since we just flushed
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(t.flushTimeout)
			}
		case <-timer.C:
			if len(buffer) > 0 {
				t.flush(buffer)
				buffer = buffer[:0]
			}
			timer.Reset(t.flushTimeout)
		case <-t.done:
			// Drain any remaining records from channel
			for {
				select {
				case record := <-t.recordCh:
					buffer = append(buffer, record)
				default:
					// Channel drained
					if len(buffer) > 0 {
						t.flush(buffer)
					}
					return
				}
			}
		}
	}
}

// Shutdown flushes remaining records and stops the tracker.
func (t *Tracker) Shutdown() {
	close(t.done)
	t.wg.Wait()
}
