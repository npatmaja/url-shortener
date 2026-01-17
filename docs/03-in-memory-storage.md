# In-Memory Storage Implementation

## Overview

The in-memory storage implementation provides a thread-safe, production-ready repository for development and testing. It implements the `Repository` interface, allowing seamless swapping to DynamoDB or Redis in production without changing business logic.

## Repository Interface

```go
package repository

import (
    "context"
    "time"

    "github.com/npatmaja/url-shortener/internal/domain"
)

// Repository defines the contract for URL storage operations.
// All implementations must be thread-safe for concurrent access.
type Repository interface {
    // SaveIfNotExists atomically saves the record only if the short code
    // doesn't already exist. Returns domain.ErrCodeExists if the code is taken.
    SaveIfNotExists(ctx context.Context, record *domain.URLRecord) error

    // FindByShortCode retrieves a record by its short code.
    // Returns domain.ErrNotFound if the code doesn't exist.
    FindByShortCode(ctx context.Context, code string) (*domain.URLRecord, error)

    // IncrementClickCount atomically increments the click counter
    // and updates LastAccessedAt timestamp.
    IncrementClickCount(ctx context.Context, code string, accessTime time.Time) error

    // DeleteExpired removes all records where ExpiresAt < before.
    // Returns the number of deleted records.
    DeleteExpired(ctx context.Context, before time.Time) (int64, error)
}
```

## In-Memory Implementation

### Data Structure

```go
package repository

import (
    "context"
    "sync"
    "time"

    "github.com/npatmaja/url-shortener/internal/domain"
)

// MemoryRepository provides thread-safe in-memory storage.
// Suitable for development, testing, and single-instance deployments.
type MemoryRepository struct {
    mu   sync.RWMutex
    data map[string]*domain.URLRecord
}

// NewMemoryRepository creates a new in-memory repository.
func NewMemoryRepository() *MemoryRepository {
    return &MemoryRepository{
        data: make(map[string]*domain.URLRecord),
    }
}
```

### SaveIfNotExists - Atomic Insert

This is the critical operation for collision handling. It must be atomic to prevent race conditions.

```go
// SaveIfNotExists atomically saves the record only if the short code
// doesn't already exist.
func (r *MemoryRepository) SaveIfNotExists(ctx context.Context, record *domain.URLRecord) error {
    // Check for context cancellation
    select {
    case <-ctx.Done():
        return ctx.Err()
    default:
    }

    r.mu.Lock()
    defer r.mu.Unlock()

    // Atomic check-and-insert within the same lock
    if _, exists := r.data[record.ShortCode]; exists {
        return domain.ErrCodeExists
    }

    // Store a copy to prevent external mutation
    r.data[record.ShortCode] = record.Clone()
    return nil
}
```

**Why this is correct:**
1. `sync.Mutex` ensures only one goroutine can execute the critical section
2. Check and insert happen within the same lock - no gap for race conditions
3. We store a clone to prevent the caller from mutating stored data

### FindByShortCode - Read with Copy

```go
// FindByShortCode retrieves a record by its short code.
func (r *MemoryRepository) FindByShortCode(ctx context.Context, code string) (*domain.URLRecord, error) {
    select {
    case <-ctx.Done():
        return nil, ctx.Err()
    default:
    }

    r.mu.RLock()
    defer r.mu.RUnlock()

    record, exists := r.data[code]
    if !exists {
        return nil, domain.ErrNotFound
    }

    // Return a copy to prevent external mutation
    return record.Clone(), nil
}
```

**Design decisions:**
1. Use `RLock` for reads - allows concurrent readers
2. Return a clone - prevents callers from corrupting stored data

### IncrementClickCount - Atomic Update

This operation is called on every redirect and must handle high concurrency.

```go
// IncrementClickCount atomically increments the click counter
// and updates the last accessed timestamp.
func (r *MemoryRepository) IncrementClickCount(ctx context.Context, code string, accessTime time.Time) error {
    select {
    case <-ctx.Done():
        return ctx.Err()
    default:
    }

    r.mu.Lock()
    defer r.mu.Unlock()

    record, exists := r.data[code]
    if !exists {
        return domain.ErrNotFound
    }

    // Atomic increment within the lock
    record.ClickCount++
    record.LastAccessedAt = accessTime

    return nil
}
```

**Thread safety analysis:**
- Lock ensures no concurrent modifications
- Read-modify-write happens atomically
- No external reference to the record, so no data race

### DeleteExpired - Batch Cleanup

Used by the background cleanup service.

```go
// DeleteExpired removes all records that have expired before the given time.
func (r *MemoryRepository) DeleteExpired(ctx context.Context, before time.Time) (int64, error) {
    select {
    case <-ctx.Done():
        return 0, ctx.Err()
    default:
    }

    r.mu.Lock()
    defer r.mu.Unlock()

    var deleted int64
    for code, record := range r.data {
        if record.ExpiresAt.Before(before) {
            delete(r.data, code)
            deleted++
        }
    }

    return deleted, nil
}
```

## Domain Entity

```go
package domain

import (
    "errors"
    "time"
)

// Domain errors
var (
    ErrNotFound   = errors.New("record not found")
    ErrCodeExists = errors.New("short code already exists")
    ErrExpired    = errors.New("record has expired")
)

// URLRecord represents a shortened URL entry.
type URLRecord struct {
    ShortCode      string
    LongURL        string
    CreatedAt      time.Time
    ExpiresAt      time.Time
    ClickCount     int64
    LastAccessedAt time.Time
}

// Clone creates a deep copy of the record.
func (r *URLRecord) Clone() *URLRecord {
    return &URLRecord{
        ShortCode:      r.ShortCode,
        LongURL:        r.LongURL,
        CreatedAt:      r.CreatedAt,
        ExpiresAt:      r.ExpiresAt,
        ClickCount:     r.ClickCount,
        LastAccessedAt: r.LastAccessedAt,
    }
}

// IsExpired checks if the record has expired relative to the given time.
func (r *URLRecord) IsExpired(now time.Time) bool {
    return now.After(r.ExpiresAt)
}
```

## Concurrency Patterns

### RWMutex Selection Guide

| Operation | Lock Type | Reasoning |
|-----------|-----------|-----------|
| SaveIfNotExists | `Lock()` | Write operation, exclusive access |
| FindByShortCode | `RLock()` | Read-only, allows concurrent readers |
| IncrementClickCount | `Lock()` | Read-modify-write, exclusive access |
| DeleteExpired | `Lock()` | Write operation, exclusive access |

### Alternative: sync.Map

For extremely high-read workloads, `sync.Map` could be considered:

```go
type SyncMapRepository struct {
    data sync.Map // map[string]*URLRecord
}
```

**Pros:**
- Lock-free reads
- Better performance for read-heavy workloads

**Cons:**
- `SaveIfNotExists` requires `LoadOrStore` which returns whether it stored
- `IncrementClickCount` requires `LoadAndDelete` + `Store` or custom atomic
- More complex implementation

**Our choice:** `sync.RWMutex` for simplicity and correctness. The performance difference is negligible for our use case, and the code is much easier to reason about.

### Alternative: Sharded Map

For extreme scale, shard the map by short code prefix:

```go
type ShardedRepository struct {
    shards [256]*shard // Shard by first byte of short code
}

type shard struct {
    mu   sync.RWMutex
    data map[string]*domain.URLRecord
}

func (r *ShardedRepository) getShard(code string) *shard {
    return r.shards[code[0]]
}
```

**This reduces lock contention but adds complexity.** Not needed unless profiling shows mutex contention.

## Thread Safety Verification Test

```go
func TestConcurrentClickCount(t *testing.T) {
    repo := NewMemoryRepository()
    ctx := context.Background()

    // Create initial record
    record := &domain.URLRecord{
        ShortCode:  "testcode",
        LongURL:    "https://example.com",
        CreatedAt:  time.Now(),
        ExpiresAt:  time.Now().Add(24 * time.Hour),
        ClickCount: 0,
    }
    err := repo.SaveIfNotExists(ctx, record)
    require.NoError(t, err)

    // Concurrent increments
    const numGoroutines = 100
    const incrementsPerGoroutine = 100

    var wg sync.WaitGroup
    wg.Add(numGoroutines)

    for i := 0; i < numGoroutines; i++ {
        go func() {
            defer wg.Done()
            for j := 0; j < incrementsPerGoroutine; j++ {
                err := repo.IncrementClickCount(ctx, "testcode", time.Now())
                assert.NoError(t, err)
            }
        }()
    }

    wg.Wait()

    // Verify final count
    result, err := repo.FindByShortCode(ctx, "testcode")
    require.NoError(t, err)
    assert.Equal(t, int64(numGoroutines*incrementsPerGoroutine), result.ClickCount)
}
```

## Memory Considerations

### Estimated Memory per Record

| Field          | Size (bytes) | Notes |
|----------------|--------------|-------|
| ShortCode      | 8 + 16       | string header + 8 chars |
| LongURL        | 16 + ~100    | string header + avg URL |
| CreatedAt      | 24           | time.Time |
| ExpiresAt      | 24           | time.Time |
| ClickCount     | 8            | int64 |
| LastAccessedAt | 24           | time.Time |
| Map overhead   | ~50          | hash bucket, pointers |

**Total: ~270 bytes per record**

### Capacity Estimates

| Records | Memory |
|---------|--------|
| 10,000 | ~2.7 MB |
| 100,000 | ~27 MB |
| 1,000,000 | ~270 MB |
| 10,000,000 | ~2.7 GB |

For development and testing, this is more than adequate. Production deployments should use DynamoDB or Redis.

## Gap Analysis: Why In-Memory Won't Work in Production

See [05-gap-analysis.md](./05-gap-analysis.md) for detailed analysis of why in-memory storage is unsuitable for the serverless cloud environment and how managed storage (DynamoDB) solves these limitations.
