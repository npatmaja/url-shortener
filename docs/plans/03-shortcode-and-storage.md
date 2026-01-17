# Plan: Short Code Generation and In-Memory Storage

## Overview

Implement the short code generator and in-memory repository following TDD. This establishes the core domain logic with proper abstractions for future storage backends (DynamoDB, Redis).

## Goals

- Define `URLRecord` domain entity
- Define `Repository` interface with atomic operations
- Implement thread-safe in-memory repository
- Implement short code generator (8 chars, excludes ambiguous: 0, O, I, l, 1)
- Handle collisions with retry mechanism
- Implement `URLService` with business logic (create, resolve, stats)

## Directory Structure

```
internal/
├── domain/
│   ├── url.go           # URLRecord entity
│   ├── errors.go        # Domain errors
│   └── clock.go         # Clock interface for testing
├── repository/
│   ├── repository.go    # Repository interface
│   ├── memory.go        # In-memory implementation
│   └── memory_test.go   # Repository tests
├── shortcode/
│   ├── generator.go     # Short code generator
│   └── generator_test.go
└── service/
    ├── url_service.go       # URLService business logic
    └── url_service_test.go  # Service tests
```

---

## Part 1: Domain Layer

### Step 1.1: Define Domain Errors

**Test First:**

```go
// internal/domain/errors_test.go

package domain_test

import (
    "errors"
    "testing"

    "url-shortener/internal/domain"

    "github.com/stretchr/testify/assert"
)

func TestErrors_AreDistinct(t *testing.T) {
    assert.False(t, errors.Is(domain.ErrNotFound, domain.ErrCodeExists))
    assert.False(t, errors.Is(domain.ErrNotFound, domain.ErrExpired))
    assert.False(t, errors.Is(domain.ErrCodeExists, domain.ErrExpired))
}

func TestErrors_CanBeWrapped(t *testing.T) {
    wrapped := fmt.Errorf("operation failed: %w", domain.ErrNotFound)
    assert.True(t, errors.Is(wrapped, domain.ErrNotFound))
}
```

**Implementation:**

```go
// internal/domain/errors.go

package domain

import "errors"

var (
    // ErrNotFound indicates the requested record does not exist.
    ErrNotFound = errors.New("record not found")

    // ErrCodeExists indicates the short code is already taken.
    ErrCodeExists = errors.New("short code already exists")

    // ErrExpired indicates the record has expired.
    ErrExpired = errors.New("record has expired")
)
```

---

### Step 1.2: Define Clock Interface

**Test First:**

```go
// internal/domain/clock_test.go

package domain_test

import (
    "testing"
    "time"

    "url-shortener/internal/domain"

    "github.com/stretchr/testify/assert"
)

func TestRealClock_ReturnsCurrentTime(t *testing.T) {
    clock := domain.RealClock{}

    before := time.Now()
    now := clock.Now()
    after := time.Now()

    assert.True(t, !now.Before(before), "clock.Now() should not be before time.Now()")
    assert.True(t, !now.After(after), "clock.Now() should not be after time.Now()")
}

func TestMockClock_ReturnsFixedTime(t *testing.T) {
    fixed := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
    clock := domain.NewMockClock(fixed)

    assert.Equal(t, fixed, clock.Now())
    assert.Equal(t, fixed, clock.Now()) // Same time on subsequent calls
}

func TestMockClock_Advance(t *testing.T) {
    fixed := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
    clock := domain.NewMockClock(fixed)

    clock.Advance(time.Hour)

    expected := time.Date(2024, 1, 15, 13, 0, 0, 0, time.UTC)
    assert.Equal(t, expected, clock.Now())
}

func TestMockClock_Set(t *testing.T) {
    clock := domain.NewMockClock(time.Now())

    newTime := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
    clock.Set(newTime)

    assert.Equal(t, newTime, clock.Now())
}
```

**Implementation:**

```go
// internal/domain/clock.go

package domain

import "time"

// Clock provides time operations for the application.
// This abstraction allows deterministic testing without time.Sleep.
type Clock interface {
    Now() time.Time
}

// RealClock implements Clock using the system time.
type RealClock struct{}

// Now returns the current system time.
func (RealClock) Now() time.Time {
    return time.Now()
}

// MockClock implements Clock with controllable time for testing.
type MockClock struct {
    current time.Time
}

// NewMockClock creates a MockClock set to the given time.
func NewMockClock(t time.Time) *MockClock {
    return &MockClock{current: t}
}

// Now returns the mock's current time.
func (c *MockClock) Now() time.Time {
    return c.current
}

// Advance moves the clock forward by the given duration.
func (c *MockClock) Advance(d time.Duration) {
    c.current = c.current.Add(d)
}

// Set sets the clock to a specific time.
func (c *MockClock) Set(t time.Time) {
    c.current = t
}
```

---

### Step 1.3: Define URLRecord Entity

**Test First:**

```go
// internal/domain/url_test.go

package domain_test

import (
    "testing"
    "time"

    "url-shortener/internal/domain"

    "github.com/stretchr/testify/assert"
)

func TestURLRecord_IsExpired(t *testing.T) {
    now := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)

    tests := []struct {
        name      string
        expiresAt time.Time
        checkTime time.Time
        want      bool
    }{
        {
            name:      "not expired - before expiry",
            expiresAt: now.Add(time.Hour),
            checkTime: now,
            want:      false,
        },
        {
            name:      "expired - after expiry",
            expiresAt: now,
            checkTime: now.Add(time.Second),
            want:      true,
        },
        {
            name:      "not expired - exactly at expiry",
            expiresAt: now,
            checkTime: now,
            want:      false,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            record := &domain.URLRecord{
                ExpiresAt: tt.expiresAt,
            }
            assert.Equal(t, tt.want, record.IsExpired(tt.checkTime))
        })
    }
}

func TestURLRecord_Clone(t *testing.T) {
    original := &domain.URLRecord{
        ShortCode:      "abc12345",
        LongURL:        "https://example.com",
        CreatedAt:      time.Now(),
        ExpiresAt:      time.Now().Add(time.Hour),
        ClickCount:     42,
        LastAccessedAt: time.Now(),
    }

    clone := original.Clone()

    // Should be equal
    assert.Equal(t, original.ShortCode, clone.ShortCode)
    assert.Equal(t, original.LongURL, clone.LongURL)
    assert.Equal(t, original.ClickCount, clone.ClickCount)

    // Should be independent (modifying clone doesn't affect original)
    clone.ClickCount = 100
    assert.Equal(t, int64(42), original.ClickCount)
}
```

**Implementation:**

```go
// internal/domain/url.go

package domain

import "time"

// URLRecord represents a shortened URL entry.
type URLRecord struct {
    ShortCode      string
    LongURL        string
    CreatedAt      time.Time
    ExpiresAt      time.Time
    ClickCount     int64
    LastAccessedAt time.Time
}

// IsExpired returns true if the record has expired at the given time.
func (r *URLRecord) IsExpired(now time.Time) bool {
    return now.After(r.ExpiresAt)
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
```

---

## Part 2: Short Code Generator

### Step 2.1: Test Character Set Exclusion

**Test First:**

```go
// internal/shortcode/generator_test.go

package shortcode_test

import (
    "strings"
    "testing"

    "url-shortener/internal/shortcode"

    "github.com/stretchr/testify/assert"
)

func TestGenerator_ExcludesAmbiguousCharacters(t *testing.T) {
    gen := shortcode.NewGenerator()
    excluded := "0OIl1"

    // Generate many codes and verify none contain excluded chars
    for i := 0; i < 10000; i++ {
        code := gen.Generate()
        for _, c := range excluded {
            assert.False(t, strings.ContainsRune(code, c),
                "code %q should not contain excluded char %q", code, string(c))
        }
    }
}

func TestGenerator_ProducesCorrectLength(t *testing.T) {
    gen := shortcode.NewGenerator()

    for i := 0; i < 1000; i++ {
        code := gen.Generate()
        assert.Len(t, code, 8, "code should be 8 characters")
    }
}

func TestGenerator_ProducesOnlyAlphanumeric(t *testing.T) {
    gen := shortcode.NewGenerator()
    allowed := "23456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

    for i := 0; i < 1000; i++ {
        code := gen.Generate()
        for _, c := range code {
            assert.True(t, strings.ContainsRune(allowed, c),
                "code %q contains invalid char %q", code, string(c))
        }
    }
}

func TestGenerator_ProducesUniqueCodesStatistically(t *testing.T) {
    gen := shortcode.NewGenerator()
    seen := make(map[string]bool)
    count := 10000

    for i := 0; i < count; i++ {
        code := gen.Generate()
        seen[code] = true
    }

    // With 34^8 possible combinations, 10000 codes should all be unique
    // (collision probability is negligible)
    assert.Len(t, seen, count, "all generated codes should be unique")
}
```

**Implementation:**

```go
// internal/shortcode/generator.go

package shortcode

import (
    "crypto/rand"
    "math/big"
)

// Alphabet excludes ambiguous characters: 0, O, I, l, 1
const alphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
const codeLength = 8

// Generator generates random short codes.
type Generator struct {
    alphabet string
    length   int
}

// NewGenerator creates a new short code generator.
func NewGenerator() *Generator {
    return &Generator{
        alphabet: alphabet,
        length:   codeLength,
    }
}

// Generate creates a new random short code.
// The code is 8 characters long using crypto/rand for security.
func (g *Generator) Generate() string {
    b := make([]byte, g.length)
    alphabetLen := big.NewInt(int64(len(g.alphabet)))

    for i := range b {
        n, err := rand.Int(rand.Reader, alphabetLen)
        if err != nil {
            // Fallback should never happen with crypto/rand
            panic("crypto/rand failed: " + err.Error())
        }
        b[i] = g.alphabet[n.Int64()]
    }

    return string(b)
}
```

---

## Part 3: In-Memory Repository

### Step 3.1: Define Repository Interface

**Implementation (Interface first - no test needed for interface definition):**

```go
// internal/repository/repository.go

package repository

import (
    "context"
    "time"

    "url-shortener/internal/domain"
)

// Repository defines the contract for URL storage operations.
// All implementations must be thread-safe for concurrent access.
type Repository interface {
    // SaveIfNotExists atomically saves the record only if the short code
    // doesn't already exist. Returns domain.ErrCodeExists if taken.
    SaveIfNotExists(ctx context.Context, record *domain.URLRecord) error

    // FindByShortCode retrieves a record by its short code.
    // Returns domain.ErrNotFound if the code doesn't exist.
    FindByShortCode(ctx context.Context, code string) (*domain.URLRecord, error)

    // IncrementClickCount atomically increments the click counter
    // and updates LastAccessedAt timestamp.
    // Returns domain.ErrNotFound if the code doesn't exist.
    IncrementClickCount(ctx context.Context, code string, accessTime time.Time) error

    // DeleteExpired removes all records where ExpiresAt < before.
    // Returns the number of deleted records.
    DeleteExpired(ctx context.Context, before time.Time) (int64, error)
}
```

---

### Step 3.2: Test SaveIfNotExists

**Test First:**

```go
// internal/repository/memory_test.go

package repository_test

import (
    "context"
    "testing"
    "time"

    "url-shortener/internal/domain"
    "url-shortener/internal/repository"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestMemoryRepository_SaveIfNotExists_Success(t *testing.T) {
    repo := repository.NewMemoryRepository()
    ctx := context.Background()

    record := &domain.URLRecord{
        ShortCode: "abc12345",
        LongURL:   "https://example.com",
        CreatedAt: time.Now(),
        ExpiresAt: time.Now().Add(time.Hour),
    }

    err := repo.SaveIfNotExists(ctx, record)
    assert.NoError(t, err)

    // Verify it was saved
    saved, err := repo.FindByShortCode(ctx, "abc12345")
    require.NoError(t, err)
    assert.Equal(t, "https://example.com", saved.LongURL)
}

func TestMemoryRepository_SaveIfNotExists_Duplicate(t *testing.T) {
    repo := repository.NewMemoryRepository()
    ctx := context.Background()

    record := &domain.URLRecord{
        ShortCode: "abc12345",
        LongURL:   "https://example.com",
    }

    // First save succeeds
    err := repo.SaveIfNotExists(ctx, record)
    require.NoError(t, err)

    // Second save with same code fails
    record2 := &domain.URLRecord{
        ShortCode: "abc12345",
        LongURL:   "https://different.com",
    }
    err = repo.SaveIfNotExists(ctx, record2)
    assert.ErrorIs(t, err, domain.ErrCodeExists)
}

func TestMemoryRepository_SaveIfNotExists_StoresClone(t *testing.T) {
    repo := repository.NewMemoryRepository()
    ctx := context.Background()

    record := &domain.URLRecord{
        ShortCode:  "abc12345",
        LongURL:    "https://example.com",
        ClickCount: 0,
    }

    err := repo.SaveIfNotExists(ctx, record)
    require.NoError(t, err)

    // Modify original after save
    record.ClickCount = 999

    // Stored record should be unaffected
    saved, _ := repo.FindByShortCode(ctx, "abc12345")
    assert.Equal(t, int64(0), saved.ClickCount)
}
```

---

### Step 3.3: Test FindByShortCode

**Test First:**

```go
func TestMemoryRepository_FindByShortCode_Success(t *testing.T) {
    repo := repository.NewMemoryRepository()
    ctx := context.Background()

    record := &domain.URLRecord{
        ShortCode:  "abc12345",
        LongURL:    "https://example.com",
        ClickCount: 42,
    }
    _ = repo.SaveIfNotExists(ctx, record)

    found, err := repo.FindByShortCode(ctx, "abc12345")
    require.NoError(t, err)
    assert.Equal(t, "abc12345", found.ShortCode)
    assert.Equal(t, "https://example.com", found.LongURL)
    assert.Equal(t, int64(42), found.ClickCount)
}

func TestMemoryRepository_FindByShortCode_NotFound(t *testing.T) {
    repo := repository.NewMemoryRepository()
    ctx := context.Background()

    _, err := repo.FindByShortCode(ctx, "notexist")
    assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestMemoryRepository_FindByShortCode_ReturnsClone(t *testing.T) {
    repo := repository.NewMemoryRepository()
    ctx := context.Background()

    record := &domain.URLRecord{
        ShortCode:  "abc12345",
        ClickCount: 10,
    }
    _ = repo.SaveIfNotExists(ctx, record)

    // Get record and modify it
    found, _ := repo.FindByShortCode(ctx, "abc12345")
    found.ClickCount = 999

    // Original in repo should be unaffected
    found2, _ := repo.FindByShortCode(ctx, "abc12345")
    assert.Equal(t, int64(10), found2.ClickCount)
}
```

---

### Step 3.4: Test IncrementClickCount

**Test First:**

```go
func TestMemoryRepository_IncrementClickCount_Success(t *testing.T) {
    repo := repository.NewMemoryRepository()
    ctx := context.Background()

    record := &domain.URLRecord{
        ShortCode:  "abc12345",
        ClickCount: 0,
    }
    _ = repo.SaveIfNotExists(ctx, record)

    accessTime := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
    err := repo.IncrementClickCount(ctx, "abc12345", accessTime)
    require.NoError(t, err)

    found, _ := repo.FindByShortCode(ctx, "abc12345")
    assert.Equal(t, int64(1), found.ClickCount)
    assert.Equal(t, accessTime, found.LastAccessedAt)
}

func TestMemoryRepository_IncrementClickCount_NotFound(t *testing.T) {
    repo := repository.NewMemoryRepository()
    ctx := context.Background()

    err := repo.IncrementClickCount(ctx, "notexist", time.Now())
    assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestMemoryRepository_IncrementClickCount_Multiple(t *testing.T) {
    repo := repository.NewMemoryRepository()
    ctx := context.Background()

    record := &domain.URLRecord{
        ShortCode:  "abc12345",
        ClickCount: 0,
    }
    _ = repo.SaveIfNotExists(ctx, record)

    for i := 0; i < 100; i++ {
        _ = repo.IncrementClickCount(ctx, "abc12345", time.Now())
    }

    found, _ := repo.FindByShortCode(ctx, "abc12345")
    assert.Equal(t, int64(100), found.ClickCount)
}
```

---

### Step 3.5: Test Concurrent Access (Critical)

**Test First:**

```go
func TestMemoryRepository_IncrementClickCount_Concurrent(t *testing.T) {
    repo := repository.NewMemoryRepository()
    ctx := context.Background()

    record := &domain.URLRecord{
        ShortCode:  "abc12345",
        ClickCount: 0,
    }
    _ = repo.SaveIfNotExists(ctx, record)

    // 100 goroutines each incrementing 100 times
    const numGoroutines = 100
    const incrementsPerGoroutine = 100
    expectedTotal := int64(numGoroutines * incrementsPerGoroutine)

    var wg sync.WaitGroup
    wg.Add(numGoroutines)

    for i := 0; i < numGoroutines; i++ {
        go func() {
            defer wg.Done()
            for j := 0; j < incrementsPerGoroutine; j++ {
                err := repo.IncrementClickCount(ctx, "abc12345", time.Now())
                assert.NoError(t, err)
            }
        }()
    }

    wg.Wait()

    found, _ := repo.FindByShortCode(ctx, "abc12345")
    assert.Equal(t, expectedTotal, found.ClickCount,
        "click count should be exactly %d after concurrent increments", expectedTotal)
}

func TestMemoryRepository_SaveIfNotExists_ConcurrentCollision(t *testing.T) {
    repo := repository.NewMemoryRepository()
    ctx := context.Background()

    const numGoroutines = 100
    code := "samecode"

    var wg sync.WaitGroup
    wg.Add(numGoroutines)

    var successCount int32
    var collisionCount int32

    for i := 0; i < numGoroutines; i++ {
        go func(id int) {
            defer wg.Done()
            record := &domain.URLRecord{
                ShortCode: code,
                LongURL:   fmt.Sprintf("https://example.com/%d", id),
            }

            err := repo.SaveIfNotExists(ctx, record)
            if err == nil {
                atomic.AddInt32(&successCount, 1)
            } else if errors.Is(err, domain.ErrCodeExists) {
                atomic.AddInt32(&collisionCount, 1)
            }
        }(i)
    }

    wg.Wait()

    // Exactly one should succeed
    assert.Equal(t, int32(1), successCount)
    assert.Equal(t, int32(numGoroutines-1), collisionCount)
}
```

---

### Step 3.6: Test DeleteExpired

**Test First:**

```go
func TestMemoryRepository_DeleteExpired(t *testing.T) {
    repo := repository.NewMemoryRepository()
    ctx := context.Background()

    now := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)

    // Create records with different expiry times
    records := []*domain.URLRecord{
        {ShortCode: "expired1", ExpiresAt: now.Add(-time.Hour)},   // Expired
        {ShortCode: "expired2", ExpiresAt: now.Add(-time.Minute)}, // Expired
        {ShortCode: "valid1", ExpiresAt: now.Add(time.Hour)},      // Valid
        {ShortCode: "valid2", ExpiresAt: now.Add(time.Minute)},    // Valid
    }

    for _, r := range records {
        _ = repo.SaveIfNotExists(ctx, r)
    }

    deleted, err := repo.DeleteExpired(ctx, now)
    require.NoError(t, err)
    assert.Equal(t, int64(2), deleted)

    // Verify expired are gone
    _, err = repo.FindByShortCode(ctx, "expired1")
    assert.ErrorIs(t, err, domain.ErrNotFound)

    _, err = repo.FindByShortCode(ctx, "expired2")
    assert.ErrorIs(t, err, domain.ErrNotFound)

    // Verify valid still exist
    _, err = repo.FindByShortCode(ctx, "valid1")
    assert.NoError(t, err)

    _, err = repo.FindByShortCode(ctx, "valid2")
    assert.NoError(t, err)
}

func TestMemoryRepository_DeleteExpired_Empty(t *testing.T) {
    repo := repository.NewMemoryRepository()
    ctx := context.Background()

    deleted, err := repo.DeleteExpired(ctx, time.Now())
    require.NoError(t, err)
    assert.Equal(t, int64(0), deleted)
}
```

---

### Step 3.7: Test Context Cancellation

**Test First:**

```go
func TestMemoryRepository_RespectsContextCancellation(t *testing.T) {
    repo := repository.NewMemoryRepository()
    ctx, cancel := context.WithCancel(context.Background())
    cancel() // Cancel immediately

    record := &domain.URLRecord{ShortCode: "test1234"}

    err := repo.SaveIfNotExists(ctx, record)
    assert.ErrorIs(t, err, context.Canceled)

    _, err = repo.FindByShortCode(ctx, "test1234")
    assert.ErrorIs(t, err, context.Canceled)

    err = repo.IncrementClickCount(ctx, "test1234", time.Now())
    assert.ErrorIs(t, err, context.Canceled)

    _, err = repo.DeleteExpired(ctx, time.Now())
    assert.ErrorIs(t, err, context.Canceled)
}
```

---

### Step 3.8: Implementation

**Implementation:**

```go
// internal/repository/memory.go

package repository

import (
    "context"
    "sync"
    "time"

    "url-shortener/internal/domain"
)

// MemoryRepository provides thread-safe in-memory storage.
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

// SaveIfNotExists atomically saves the record only if the short code
// doesn't already exist.
func (r *MemoryRepository) SaveIfNotExists(ctx context.Context, record *domain.URLRecord) error {
    select {
    case <-ctx.Done():
        return ctx.Err()
    default:
    }

    r.mu.Lock()
    defer r.mu.Unlock()

    if _, exists := r.data[record.ShortCode]; exists {
        return domain.ErrCodeExists
    }

    r.data[record.ShortCode] = record.Clone()
    return nil
}

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

    return record.Clone(), nil
}

// IncrementClickCount atomically increments the click counter.
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

    record.ClickCount++
    record.LastAccessedAt = accessTime
    return nil
}

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

---

## Part 4: URL Service

The URLService orchestrates the repository and short code generator, implementing the core business logic.

### Step 4.1: Define Service Interface and Struct

**Implementation (Structure first):**

```go
// internal/service/url_service.go

package service

import (
    "context"
    "time"

    "url-shortener/internal/domain"
    "url-shortener/internal/repository"
    "url-shortener/internal/shortcode"
)

const (
    maxRetries = 5
    defaultTTL = 24 * time.Hour
)

// URLService handles URL shortening business logic.
type URLService struct {
    repo      repository.Repository
    generator *shortcode.Generator
    clock     domain.Clock
}

// NewURLService creates a new URLService.
func NewURLService(repo repository.Repository, generator *shortcode.Generator, clock domain.Clock) *URLService {
    return &URLService{
        repo:      repo,
        generator: generator,
        clock:     clock,
    }
}
```

---

### Step 4.2: Test Create - Success

**Test First:**

```go
// internal/service/url_service_test.go

package service_test

import (
    "context"
    "testing"
    "time"

    "url-shortener/internal/domain"
    "url-shortener/internal/repository"
    "url-shortener/internal/service"
    "url-shortener/internal/shortcode"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestURLService_Create_Success(t *testing.T) {
    repo := repository.NewMemoryRepository()
    gen := shortcode.NewGenerator()
    clock := domain.NewMockClock(time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC))

    svc := service.NewURLService(repo, gen, clock)

    record, err := svc.Create(context.Background(), "https://example.com", time.Hour)
    require.NoError(t, err)

    assert.Len(t, record.ShortCode, 8)
    assert.Equal(t, "https://example.com", record.LongURL)
    assert.Equal(t, clock.Now(), record.CreatedAt)
    assert.Equal(t, clock.Now().Add(time.Hour), record.ExpiresAt)
    assert.Equal(t, int64(0), record.ClickCount)
}

func TestURLService_Create_UsesDefaultTTL(t *testing.T) {
    repo := repository.NewMemoryRepository()
    gen := shortcode.NewGenerator()
    clock := domain.NewMockClock(time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC))

    svc := service.NewURLService(repo, gen, clock)

    // Pass 0 duration to use default
    record, err := svc.Create(context.Background(), "https://example.com", 0)
    require.NoError(t, err)

    // Default TTL is 24 hours
    assert.Equal(t, clock.Now().Add(24*time.Hour), record.ExpiresAt)
}

func TestURLService_Create_StoresInRepository(t *testing.T) {
    repo := repository.NewMemoryRepository()
    gen := shortcode.NewGenerator()
    clock := domain.NewMockClock(time.Now())

    svc := service.NewURLService(repo, gen, clock)

    record, err := svc.Create(context.Background(), "https://example.com", time.Hour)
    require.NoError(t, err)

    // Verify stored in repository
    stored, err := repo.FindByShortCode(context.Background(), record.ShortCode)
    require.NoError(t, err)
    assert.Equal(t, record.LongURL, stored.LongURL)
}
```

**Implementation:**

```go
// Create creates a new shortened URL with the given TTL.
// If ttl is 0, the default TTL (24 hours) is used.
// Returns the created record or an error if max retries exceeded.
func (s *URLService) Create(ctx context.Context, longURL string, ttl time.Duration) (*domain.URLRecord, error) {
    if ttl == 0 {
        ttl = defaultTTL
    }

    now := s.clock.Now()

    for attempt := 0; attempt < maxRetries; attempt++ {
        code := s.generator.Generate()

        record := &domain.URLRecord{
            ShortCode:      code,
            LongURL:        longURL,
            CreatedAt:      now,
            ExpiresAt:      now.Add(ttl),
            ClickCount:     0,
            LastAccessedAt: time.Time{},
        }

        err := s.repo.SaveIfNotExists(ctx, record)
        if err == nil {
            return record, nil
        }

        if errors.Is(err, domain.ErrCodeExists) {
            continue // Collision, retry with new code
        }

        return nil, fmt.Errorf("saving record: %w", err)
    }

    return nil, errors.New("max retries exceeded: unable to generate unique code")
}
```

---

### Step 4.3: Test Create - Collision Retry

**Test First:**

```go
func TestURLService_Create_RetriesOnCollision(t *testing.T) {
    repo := repository.NewMemoryRepository()
    clock := domain.NewMockClock(time.Now())

    // Use a mock generator that returns predictable codes
    mockGen := &MockGenerator{
        codes: []string{"code0001", "code0001", "code0001", "code0004"},
    }

    svc := service.NewURLServiceWithGenerator(repo, mockGen, clock)

    // First create succeeds with code0001
    record1, err := svc.Create(context.Background(), "https://first.com", time.Hour)
    require.NoError(t, err)
    assert.Equal(t, "code0001", record1.ShortCode)

    // Second create: code0001 collides, code0001 collides, code0001 collides, code0004 succeeds
    record2, err := svc.Create(context.Background(), "https://second.com", time.Hour)
    require.NoError(t, err)
    assert.Equal(t, "code0004", record2.ShortCode)
}

// MockGenerator for testing collision scenarios
type MockGenerator struct {
    codes []string
    index int
}

func (m *MockGenerator) Generate() string {
    if m.index >= len(m.codes) {
        return fmt.Sprintf("fallback%d", m.index)
    }
    code := m.codes[m.index]
    m.index++
    return code
}
```

---

### Step 4.4: Test Create - Max Retries Exceeded

**Test First:**

```go
func TestURLService_Create_FailsAfterMaxRetries(t *testing.T) {
    repo := repository.NewMemoryRepository()
    clock := domain.NewMockClock(time.Now())

    // Generator always returns same code
    mockGen := &MockGenerator{
        codes: []string{"samecode", "samecode", "samecode", "samecode", "samecode", "samecode"},
    }

    svc := service.NewURLServiceWithGenerator(repo, mockGen, clock)

    // First create succeeds
    _, err := svc.Create(context.Background(), "https://first.com", time.Hour)
    require.NoError(t, err)

    // Second create fails after 5 retries (all collide)
    _, err = svc.Create(context.Background(), "https://second.com", time.Hour)
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "max retries exceeded")
}
```

---

### Step 4.5: Test Resolve - Success

**Test First:**

```go
func TestURLService_Resolve_Success(t *testing.T) {
    repo := repository.NewMemoryRepository()
    gen := shortcode.NewGenerator()
    clock := domain.NewMockClock(time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC))

    svc := service.NewURLService(repo, gen, clock)

    // Create a URL
    record, _ := svc.Create(context.Background(), "https://example.com", time.Hour)

    // Resolve it
    longURL, err := svc.Resolve(context.Background(), record.ShortCode)
    require.NoError(t, err)
    assert.Equal(t, "https://example.com", longURL)
}

func TestURLService_Resolve_IncrementsClickCount(t *testing.T) {
    repo := repository.NewMemoryRepository()
    gen := shortcode.NewGenerator()
    clock := domain.NewMockClock(time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC))

    svc := service.NewURLService(repo, gen, clock)

    record, _ := svc.Create(context.Background(), "https://example.com", time.Hour)

    // Resolve multiple times
    for i := 0; i < 5; i++ {
        _, err := svc.Resolve(context.Background(), record.ShortCode)
        require.NoError(t, err)
    }

    // Check click count
    stats, _ := svc.GetStats(context.Background(), record.ShortCode)
    assert.Equal(t, int64(5), stats.ClickCount)
}

func TestURLService_Resolve_UpdatesLastAccessedAt(t *testing.T) {
    repo := repository.NewMemoryRepository()
    gen := shortcode.NewGenerator()
    clock := domain.NewMockClock(time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC))

    svc := service.NewURLService(repo, gen, clock)

    record, _ := svc.Create(context.Background(), "https://example.com", time.Hour)

    // Advance clock
    clock.Advance(30 * time.Minute)

    // Resolve
    _, _ = svc.Resolve(context.Background(), record.ShortCode)

    // Check LastAccessedAt
    stats, _ := svc.GetStats(context.Background(), record.ShortCode)
    assert.Equal(t, clock.Now(), stats.LastAccessedAt)
}
```

**Implementation:**

```go
// Resolve returns the long URL for the given short code.
// It increments the click count and updates LastAccessedAt.
// Returns domain.ErrNotFound if not found, domain.ErrExpired if expired.
func (s *URLService) Resolve(ctx context.Context, shortCode string) (string, error) {
    record, err := s.repo.FindByShortCode(ctx, shortCode)
    if err != nil {
        return "", err
    }

    // Check expiration
    if record.IsExpired(s.clock.Now()) {
        return "", domain.ErrExpired
    }

    // Increment click count (fire and forget - don't block redirect)
    _ = s.repo.IncrementClickCount(ctx, shortCode, s.clock.Now())

    return record.LongURL, nil
}
```

---

### Step 4.6: Test Resolve - Not Found

**Test First:**

```go
func TestURLService_Resolve_NotFound(t *testing.T) {
    repo := repository.NewMemoryRepository()
    gen := shortcode.NewGenerator()
    clock := domain.NewMockClock(time.Now())

    svc := service.NewURLService(repo, gen, clock)

    _, err := svc.Resolve(context.Background(), "notexist")
    assert.ErrorIs(t, err, domain.ErrNotFound)
}
```

---

### Step 4.7: Test Resolve - Expired (Deterministic with MockClock)

**Test First:**

```go
func TestURLService_Resolve_Expired(t *testing.T) {
    repo := repository.NewMemoryRepository()
    gen := shortcode.NewGenerator()
    clock := domain.NewMockClock(time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC))

    svc := service.NewURLService(repo, gen, clock)

    // Create URL with 1 hour TTL
    record, _ := svc.Create(context.Background(), "https://example.com", time.Hour)

    // URL works before expiration
    _, err := svc.Resolve(context.Background(), record.ShortCode)
    require.NoError(t, err)

    // Advance clock past expiration
    clock.Advance(time.Hour + time.Second)

    // URL is now expired
    _, err = svc.Resolve(context.Background(), record.ShortCode)
    assert.ErrorIs(t, err, domain.ErrExpired)
}

func TestURLService_Resolve_JustBeforeExpiration(t *testing.T) {
    repo := repository.NewMemoryRepository()
    gen := shortcode.NewGenerator()
    clock := domain.NewMockClock(time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC))

    svc := service.NewURLService(repo, gen, clock)

    record, _ := svc.Create(context.Background(), "https://example.com", time.Hour)

    // Advance to 1 second before expiration
    clock.Advance(time.Hour - time.Second)

    // Should still work
    longURL, err := svc.Resolve(context.Background(), record.ShortCode)
    require.NoError(t, err)
    assert.Equal(t, "https://example.com", longURL)
}
```

---

### Step 4.8: Test GetStats

**Test First:**

```go
func TestURLService_GetStats_Success(t *testing.T) {
    repo := repository.NewMemoryRepository()
    gen := shortcode.NewGenerator()
    clock := domain.NewMockClock(time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC))

    svc := service.NewURLService(repo, gen, clock)

    record, _ := svc.Create(context.Background(), "https://example.com", time.Hour)

    stats, err := svc.GetStats(context.Background(), record.ShortCode)
    require.NoError(t, err)

    assert.Equal(t, record.ShortCode, stats.ShortCode)
    assert.Equal(t, "https://example.com", stats.LongURL)
    assert.Equal(t, clock.Now(), stats.CreatedAt)
    assert.Equal(t, clock.Now().Add(time.Hour), stats.ExpiresAt)
    assert.Equal(t, int64(0), stats.ClickCount)
    assert.True(t, stats.LastAccessedAt.IsZero())
}

func TestURLService_GetStats_NotFound(t *testing.T) {
    repo := repository.NewMemoryRepository()
    gen := shortcode.NewGenerator()
    clock := domain.NewMockClock(time.Now())

    svc := service.NewURLService(repo, gen, clock)

    _, err := svc.GetStats(context.Background(), "notexist")
    assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestURLService_GetStats_Expired(t *testing.T) {
    repo := repository.NewMemoryRepository()
    gen := shortcode.NewGenerator()
    clock := domain.NewMockClock(time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC))

    svc := service.NewURLService(repo, gen, clock)

    record, _ := svc.Create(context.Background(), "https://example.com", time.Hour)

    // Advance past expiration
    clock.Advance(2 * time.Hour)

    _, err := svc.GetStats(context.Background(), record.ShortCode)
    assert.ErrorIs(t, err, domain.ErrExpired)
}
```

**Implementation:**

```go
// GetStats returns the full record for the given short code.
// Returns domain.ErrNotFound if not found, domain.ErrExpired if expired.
func (s *URLService) GetStats(ctx context.Context, shortCode string) (*domain.URLRecord, error) {
    record, err := s.repo.FindByShortCode(ctx, shortCode)
    if err != nil {
        return nil, err
    }

    if record.IsExpired(s.clock.Now()) {
        return nil, domain.ErrExpired
    }

    return record, nil
}
```

---

### Step 4.9: Generator Interface for Testing

To support mock generators for collision testing:

```go
// internal/service/url_service.go

// CodeGenerator defines the interface for short code generation.
type CodeGenerator interface {
    Generate() string
}

// URLService handles URL shortening business logic.
type URLService struct {
    repo      repository.Repository
    generator CodeGenerator
    clock     domain.Clock
}

// NewURLService creates a new URLService with the default generator.
func NewURLService(repo repository.Repository, generator *shortcode.Generator, clock domain.Clock) *URLService {
    return &URLService{
        repo:      repo,
        generator: generator,
        clock:     clock,
    }
}

// NewURLServiceWithGenerator creates a URLService with a custom generator (for testing).
func NewURLServiceWithGenerator(repo repository.Repository, generator CodeGenerator, clock domain.Clock) *URLService {
    return &URLService{
        repo:      repo,
        generator: generator,
        clock:     clock,
    }
}
```

---

## Checklist

### Part 1: Domain Layer
- [x] Write test: Domain errors are distinct and can be wrapped
- [x] Implement: domain/errors.go
- [x] Write test: RealClock returns current time
- [x] Write test: MockClock returns fixed time, Advance, Set
- [x] Implement: domain/clock.go
- [x] Write test: URLRecord.IsExpired
- [x] Write test: URLRecord.Clone creates independent copy
- [x] Implement: domain/url.go

### Part 2: Short Code Generator
- [x] Write test: Excludes ambiguous characters (0, O, I, l, 1)
- [x] Write test: Produces 8-character codes
- [x] Write test: Uses only alphanumeric characters
- [x] Write test: Produces statistically unique codes
- [x] Implement: shortcode/generator.go

### Part 3: In-Memory Repository
- [x] Define: Repository interface
- [x] Write test: SaveIfNotExists success
- [x] Write test: SaveIfNotExists duplicate returns ErrCodeExists
- [x] Write test: SaveIfNotExists stores clone
- [x] Write test: FindByShortCode success
- [x] Write test: FindByShortCode not found
- [x] Write test: FindByShortCode returns clone
- [x] Write test: IncrementClickCount success
- [x] Write test: IncrementClickCount not found
- [x] Write test: IncrementClickCount multiple times
- [x] Write test: **Concurrent IncrementClickCount (100 goroutines x 100)**
- [x] Write test: **Concurrent SaveIfNotExists collision**
- [x] Write test: DeleteExpired removes only expired
- [x] Write test: DeleteExpired on empty repo
- [x] Write test: Context cancellation respected
- [x] Implement: repository/memory.go
- [x] Run all tests with race detector

### Part 4: URL Service
- [x] Define: CodeGenerator interface
- [x] Define: URLService struct
- [x] Write test: Create success with custom TTL
- [x] Write test: Create uses default TTL when 0
- [x] Write test: Create stores in repository
- [x] Write test: Create retries on collision (with MockGenerator)
- [x] Write test: Create fails after max retries
- [x] Implement: URLService.Create
- [x] Write test: Resolve success returns long URL
- [x] Write test: Resolve increments click count
- [x] Write test: Resolve updates LastAccessedAt
- [x] Write test: Resolve not found
- [x] Write test: Resolve expired (deterministic with MockClock)
- [x] Write test: Resolve just before expiration
- [x] Implement: URLService.Resolve
- [x] Write test: GetStats success
- [x] Write test: GetStats not found
- [x] Write test: GetStats expired
- [x] Implement: URLService.GetStats
- [x] Run all tests with race detector
