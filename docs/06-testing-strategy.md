# Testing Strategy

## Philosophy: Signal Over Coverage

This project prioritizes **high-signal tests** over line coverage percentages. We focus on three critical areas:

1. **Concurrency Validation** - Prove thread safety under load
2. **Deterministic Expiration** - Test TTL logic without real delays
3. **Interface Verification** - Test domain logic against abstractions

## Test Categories

### 1. Concurrency Validation Tests

These tests verify that the `click_count` and other shared state operations are safe under concurrent access.

```go
package repository_test

import (
    "context"
    "sync"
    "testing"
    "time"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/npatmaja/url-shortener/internal/domain"
    "github.com/npatmaja/url-shortener/internal/repository"
)

func TestIncrementClickCount_ConcurrentAccess(t *testing.T) {
    repo := repository.NewMemoryRepository()
    ctx := context.Background()

    // Setup: Create a URL record
    record := &domain.URLRecord{
        ShortCode:  "testcode",
        LongURL:    "https://example.com",
        CreatedAt:  time.Now(),
        ExpiresAt:  time.Now().Add(24 * time.Hour),
        ClickCount: 0,
    }
    err := repo.SaveIfNotExists(ctx, record)
    require.NoError(t, err)

    // Test: 100 goroutines each incrementing 100 times
    const numGoroutines = 100
    const incrementsPerGoroutine = 100
    expectedTotal := int64(numGoroutines * incrementsPerGoroutine)

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

    // Verify: Final count equals expected total
    result, err := repo.FindByShortCode(ctx, "testcode")
    require.NoError(t, err)
    assert.Equal(t, expectedTotal, result.ClickCount,
        "click_count should be exactly %d after %d concurrent increments",
        expectedTotal, expectedTotal)
}

func TestSaveIfNotExists_ConcurrentCollision(t *testing.T) {
    repo := repository.NewMemoryRepository()
    ctx := context.Background()

    const numGoroutines = 100
    code := "collision"

    var wg sync.WaitGroup
    wg.Add(numGoroutines)

    successCount := int32(0)
    collisionCount := int32(0)

    for i := 0; i < numGoroutines; i++ {
        go func(id int) {
            defer wg.Done()
            record := &domain.URLRecord{
                ShortCode: code,
                LongURL:   fmt.Sprintf("https://example.com/%d", id),
                CreatedAt: time.Now(),
                ExpiresAt: time.Now().Add(time.Hour),
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

    // Exactly one goroutine should succeed
    assert.Equal(t, int32(1), successCount, "exactly one insert should succeed")
    assert.Equal(t, int32(numGoroutines-1), collisionCount, "all others should get collision")
}
```

### 2. Deterministic Expiration Tests

These tests mock the system clock to verify TTL logic without using `time.Sleep`.

#### Clock Interface

```go
package domain

import "time"

// Clock provides time operations, enabling deterministic testing.
type Clock interface {
    Now() time.Time
}

// RealClock uses the actual system time.
type RealClock struct{}

func (RealClock) Now() time.Time {
    return time.Now()
}

// MockClock allows controlling time in tests.
type MockClock struct {
    current time.Time
}

func NewMockClock(t time.Time) *MockClock {
    return &MockClock{current: t}
}

func (c *MockClock) Now() time.Time {
    return c.current
}

func (c *MockClock) Advance(d time.Duration) {
    c.current = c.current.Add(d)
}

func (c *MockClock) Set(t time.Time) {
    c.current = t
}
```

#### Deterministic TTL Tests

```go
package service_test

import (
    "context"
    "testing"
    "time"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/npatmaja/url-shortener/internal/domain"
    "github.com/npatmaja/url-shortener/internal/repository"
    "github.com/npatmaja/url-shortener/internal/service"
)

func TestResolve_ExpiredURL_ReturnsError(t *testing.T) {
    // Setup with mock clock
    baseTime := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
    clock := domain.NewMockClock(baseTime)
    repo := repository.NewMemoryRepository()
    svc := service.NewURLService(repo, clock)

    ctx := context.Background()

    // Create URL with 1 hour TTL
    record, err := svc.Create(ctx, "https://example.com", time.Hour)
    require.NoError(t, err)

    // Verify URL works before expiration
    longURL, err := svc.Resolve(ctx, record.ShortCode)
    require.NoError(t, err)
    assert.Equal(t, "https://example.com", longURL)

    // Advance clock past expiration (no sleep!)
    clock.Advance(time.Hour + time.Second)

    // Verify URL is now expired
    _, err = svc.Resolve(ctx, record.ShortCode)
    assert.ErrorIs(t, err, domain.ErrExpired)
}

func TestResolve_JustBeforeExpiration_Succeeds(t *testing.T) {
    baseTime := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
    clock := domain.NewMockClock(baseTime)
    repo := repository.NewMemoryRepository()
    svc := service.NewURLService(repo, clock)

    ctx := context.Background()

    // Create URL with 1 hour TTL
    record, err := svc.Create(ctx, "https://example.com", time.Hour)
    require.NoError(t, err)

    // Advance clock to 1 second before expiration
    clock.Advance(time.Hour - time.Second)

    // URL should still work
    longURL, err := svc.Resolve(ctx, record.ShortCode)
    require.NoError(t, err)
    assert.Equal(t, "https://example.com", longURL)
}

func TestDeleteExpired_RemovesOnlyExpiredRecords(t *testing.T) {
    baseTime := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
    clock := domain.NewMockClock(baseTime)
    repo := repository.NewMemoryRepository()

    ctx := context.Background()

    // Create records with different TTLs
    records := []struct {
        code string
        ttl  time.Duration
    }{
        {"short1", time.Hour},      // Expires in 1 hour
        {"short2", 2 * time.Hour},  // Expires in 2 hours
        {"short3", 30 * time.Minute}, // Expires in 30 min
    }

    for _, r := range records {
        record := &domain.URLRecord{
            ShortCode: r.code,
            LongURL:   "https://example.com/" + r.code,
            CreatedAt: clock.Now(),
            ExpiresAt: clock.Now().Add(r.ttl),
        }
        err := repo.SaveIfNotExists(ctx, record)
        require.NoError(t, err)
    }

    // Advance clock by 1.5 hours
    clock.Advance(90 * time.Minute)

    // Delete expired records
    deleted, err := repo.DeleteExpired(ctx, clock.Now())
    require.NoError(t, err)

    // Should delete short1 (1h) and short3 (30min), but not short2 (2h)
    assert.Equal(t, int64(2), deleted)

    // Verify short2 still exists
    _, err = repo.FindByShortCode(ctx, "short2")
    assert.NoError(t, err)

    // Verify short1 is gone
    _, err = repo.FindByShortCode(ctx, "short1")
    assert.ErrorIs(t, err, domain.ErrNotFound)
}
```

### 3. Interface Verification Tests

These tests verify that domain logic is tested against the storage abstraction, not a concrete implementation.

```go
package service_test

import (
    "context"
    "testing"
    "time"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/mock"
    "github.com/stretchr/testify/require"

    "github.com/npatmaja/url-shortener/internal/domain"
    "github.com/npatmaja/url-shortener/internal/service"
)

// MockRepository implements Repository interface for testing.
type MockRepository struct {
    mock.Mock
}

func (m *MockRepository) SaveIfNotExists(ctx context.Context, record *domain.URLRecord) error {
    args := m.Called(ctx, record)
    return args.Error(0)
}

func (m *MockRepository) FindByShortCode(ctx context.Context, code string) (*domain.URLRecord, error) {
    args := m.Called(ctx, code)
    if args.Get(0) == nil {
        return nil, args.Error(1)
    }
    return args.Get(0).(*domain.URLRecord), args.Error(1)
}

func (m *MockRepository) IncrementClickCount(ctx context.Context, code string, accessTime time.Time) error {
    args := m.Called(ctx, code, accessTime)
    return args.Error(0)
}

func (m *MockRepository) DeleteExpired(ctx context.Context, before time.Time) (int64, error) {
    args := m.Called(ctx, before)
    return args.Get(0).(int64), args.Error(1)
}

func TestURLService_Create_CallsRepositoryWithCorrectData(t *testing.T) {
    mockRepo := new(MockRepository)
    clock := domain.NewMockClock(time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC))
    svc := service.NewURLService(mockRepo, clock)

    ctx := context.Background()

    // Expect SaveIfNotExists to be called
    mockRepo.On("SaveIfNotExists", ctx, mock.MatchedBy(func(r *domain.URLRecord) bool {
        return r.LongURL == "https://example.com" &&
            r.ExpiresAt.Equal(clock.Now().Add(time.Hour)) &&
            len(r.ShortCode) == 8
    })).Return(nil)

    record, err := svc.Create(ctx, "https://example.com", time.Hour)
    require.NoError(t, err)
    assert.NotEmpty(t, record.ShortCode)

    mockRepo.AssertExpectations(t)
}

func TestURLService_Resolve_IncrementsClickCount(t *testing.T) {
    mockRepo := new(MockRepository)
    clock := domain.NewMockClock(time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC))
    svc := service.NewURLService(mockRepo, clock)

    ctx := context.Background()

    existingRecord := &domain.URLRecord{
        ShortCode:  "testcode",
        LongURL:    "https://example.com",
        CreatedAt:  clock.Now().Add(-time.Hour),
        ExpiresAt:  clock.Now().Add(time.Hour),
        ClickCount: 5,
    }

    mockRepo.On("FindByShortCode", ctx, "testcode").Return(existingRecord, nil)
    mockRepo.On("IncrementClickCount", ctx, "testcode", clock.Now()).Return(nil)

    longURL, err := svc.Resolve(ctx, "testcode")
    require.NoError(t, err)
    assert.Equal(t, "https://example.com", longURL)

    mockRepo.AssertExpectations(t)
}

func TestURLService_Create_RetriesOnCollision(t *testing.T) {
    mockRepo := new(MockRepository)
    clock := domain.NewMockClock(time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC))
    svc := service.NewURLService(mockRepo, clock)

    ctx := context.Background()

    // First 2 attempts collide, third succeeds
    callCount := 0
    mockRepo.On("SaveIfNotExists", ctx, mock.Anything).Return(func(ctx context.Context, r *domain.URLRecord) error {
        callCount++
        if callCount < 3 {
            return domain.ErrCodeExists
        }
        return nil
    })

    record, err := svc.Create(ctx, "https://example.com", time.Hour)
    require.NoError(t, err)
    assert.NotEmpty(t, record.ShortCode)
    assert.Equal(t, 3, callCount, "should have retried 3 times")
}
```

### 4. Handler Tests (HTTP Layer)

```go
package handler_test

import (
    "bytes"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/npatmaja/url-shortener/internal/handler"
)

func TestCreateHandler_ValidURL_Returns201(t *testing.T) {
    svc := setupTestService()
    h := handler.NewHandler(svc, "http://localhost:8080")

    body := `{"long_url": "https://example.com", "ttl_seconds": 3600}`
    req := httptest.NewRequest(http.MethodPost, "/shorten", bytes.NewBufferString(body))
    req.Header.Set("Content-Type", "application/json")

    rec := httptest.NewRecorder()
    h.CreateShortURL(rec, req)

    assert.Equal(t, http.StatusCreated, rec.Code)

    var resp handler.CreateResponse
    err := json.Unmarshal(rec.Body.Bytes(), &resp)
    require.NoError(t, err)
    assert.Len(t, resp.ShortCode, 8)
    assert.Equal(t, "https://example.com", resp.LongURL)

    // Verify X-Processing-Time-Micros header
    assert.NotEmpty(t, rec.Header().Get("X-Processing-Time-Micros"))
}

func TestCreateHandler_InvalidURL_Returns400(t *testing.T) {
    svc := setupTestService()
    h := handler.NewHandler(svc, "http://localhost:8080")

    testCases := []struct {
        name    string
        body    string
        wantMsg string
    }{
        {
            name:    "missing scheme",
            body:    `{"long_url": "example.com"}`,
            wantMsg: "URL scheme must be http or https",
        },
        {
            name:    "ftp scheme",
            body:    `{"long_url": "ftp://example.com"}`,
            wantMsg: "URL scheme must be http or https",
        },
        {
            name:    "empty URL",
            body:    `{"long_url": ""}`,
            wantMsg: "long_url is required",
        },
    }

    for _, tc := range testCases {
        t.Run(tc.name, func(t *testing.T) {
            req := httptest.NewRequest(http.MethodPost, "/shorten", bytes.NewBufferString(tc.body))
            req.Header.Set("Content-Type", "application/json")

            rec := httptest.NewRecorder()
            h.CreateShortURL(rec, req)

            assert.Equal(t, http.StatusBadRequest, rec.Code)

            var resp handler.ErrorResponse
            err := json.Unmarshal(rec.Body.Bytes(), &resp)
            require.NoError(t, err)
            assert.Contains(t, resp.Message, tc.wantMsg)
        })
    }
}

func TestRedirectHandler_ValidCode_Returns302(t *testing.T) {
    svc := setupTestService()
    h := handler.NewHandler(svc, "http://localhost:8080")

    // First create a URL
    record, _ := svc.Create(context.Background(), "https://example.com", time.Hour)

    req := httptest.NewRequest(http.MethodGet, "/s/"+record.ShortCode, nil)
    req.SetPathValue("code", record.ShortCode)

    rec := httptest.NewRecorder()
    h.Redirect(rec, req)

    assert.Equal(t, http.StatusFound, rec.Code)
    assert.Equal(t, "https://example.com", rec.Header().Get("Location"))
}

func TestRedirectHandler_ExpiredCode_Returns404(t *testing.T) {
    clock := domain.NewMockClock(time.Now())
    repo := repository.NewMemoryRepository()
    svc := service.NewURLService(repo, clock)
    h := handler.NewHandler(svc, "http://localhost:8080")

    // Create URL with short TTL
    record, _ := svc.Create(context.Background(), "https://example.com", time.Minute)

    // Advance time past expiration
    clock.Advance(2 * time.Minute)

    req := httptest.NewRequest(http.MethodGet, "/s/"+record.ShortCode, nil)
    req.SetPathValue("code", record.ShortCode)

    rec := httptest.NewRecorder()
    h.Redirect(rec, req)

    assert.Equal(t, http.StatusNotFound, rec.Code)
}
```

## Running Tests

```bash
# Run all tests
go test ./...

# Run with race detector (important for concurrency tests)
go test -race ./...

# Run specific test
go test -v -run TestIncrementClickCount_ConcurrentAccess ./internal/repository

# Run with coverage (for reference, not a quality metric)
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

## Test Organization

```
internal/
├── domain/
│   └── domain_test.go         # Entity tests
├── service/
│   ├── service_test.go        # Unit tests with mocks
│   └── integration_test.go    # Tests with real repository
├── repository/
│   └── repository_test.go     # Concurrency tests
└── handler/
    └── handler_test.go        # HTTP handler tests
```
