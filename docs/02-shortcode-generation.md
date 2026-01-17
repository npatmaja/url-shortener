# Short Code Generation Strategy

## Overview

The short code generator creates 8-character alphanumeric codes that are:
- Human-readable (excludes ambiguous characters: `0`, `O`, `I`, `l`, `1`)
- Unique across the system
- URL-safe

## Character Set

**Allowed Characters (34 total):**
```
2 3 4 5 6 7 8 9
A B C D E F G H J K L M N P Q R S T U V W X Y Z
a b c d e f g h i j k m n o p q r s t u v w x y z
```

**Excluded Characters:**
- `0` (zero) - confused with `O`
- `O` (uppercase O) - confused with `0`
- `I` (uppercase I) - confused with `l` and `1`
- `l` (lowercase L) - confused with `I` and `1`
- `1` (one) - confused with `I` and `l`

**Total Combinations:** 34^8 = **1,785,793,904,896** (~1.78 trillion)

## Generation Strategies Comparison

### Strategy 1: Random Generation with Collision Check (Recommended)

This approach generates a random code and verifies uniqueness before accepting.

**Algorithm:**
1. Generate 8 random characters from the allowed alphabet using crypto/rand
2. Attempt to insert with conditional write (atomic "save if not exists")
3. If collision detected, retry with new random code
4. Maximum 5 retries before returning error

```go
const alphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
const codeLength = 8

func (g *RandomGenerator) Generate() string {
    b := make([]byte, codeLength)
    for i := range b {
        n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
        b[i] = alphabet[n.Int64()]
    }
    return string(b)
}
```

**Collision Probability Analysis (Birthday Problem):**

| Total URLs | Collision Probability | Expected Retries |
|------------|----------------------|------------------|
| 1 million  | 0.00003%             | 1.0000003        |
| 10 million | 0.003%               | 1.00003          |
| 100 million| 0.28%                | 1.003            |
| 1 billion  | 24.5%                | 1.32             |

**Pros:**
- Simple implementation
- Cryptographically secure (unpredictable codes)
- No coordination needed between instances
- Stateless generation

**Cons:**
- Requires existence check (1 read/write operation per attempt)
- Collision probability increases with data size (manageable)

---

### Strategy 2: Counter-Based with Encoding

Use a monotonic counter and encode the value into base-34.

**Pros:**
- Zero collision by design
- No existence check needed

**Cons:**
- Sequential codes are predictable (security/enumeration risk)
- Requires distributed coordination for multi-instance deployment
- Counter state must be persistent and highly available

---

### Strategy 3: Hash-Based with Collision Resolution

Hash the long URL to derive the short code.

**Pros:**
- Same URL always gets same code (idempotent)
- Deduplication built-in

**Cons:**
- Hash collisions require complex resolution
- Different URLs can produce same hash prefix
- Not suitable when same URL should get different codes

---

## Recommended Implementation: Optimistic Insert Pattern

We use **Strategy 1 (Random)** with atomic conditional writes to handle race conditions.

### Why Optimistic Insert?

A naive "check-then-insert" pattern has a race condition:
```
Thread A: Check "abc123" -> not exists
Thread B: Check "abc123" -> not exists
Thread A: Insert "abc123" -> success
Thread B: Insert "abc123" -> DUPLICATE! (data corruption)
```

The optimistic insert pattern solves this:
```
Thread A: Insert "abc123" with condition "not exists" -> success
Thread B: Insert "abc123" with condition "not exists" -> FAILS (retry)
```

### Implementation Flow

```
┌─────────────────────────────────────────────────────────────┐
│                   Create Short URL                           │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
                 ┌────────────────────────┐
                 │ Generate Random Code   │
                 │ (8 chars, crypto/rand) │
                 └───────────┬────────────┘
                             │
                             ▼
                 ┌────────────────────────┐
                 │ SaveIfNotExists()      │
                 │ (Atomic conditional    │
                 │  write operation)      │
                 └───────────┬────────────┘
                             │
              ┌──────────────┴──────────────┐
              │                             │
              ▼                             ▼
    ┌─────────────────┐          ┌─────────────────┐
    │ Success:        │          │ ErrCodeExists:  │
    │ Return record   │          │ Code collision  │
    └─────────────────┘          └────────┬────────┘
                                          │
                                          ▼
                                ┌─────────────────┐
                                │ Retry count < 5?│
                                └────────┬────────┘
                                   │           │
                                  Yes          No
                                   │           │
                                   ▼           ▼
                          ┌────────────┐  ┌──────────────┐
                          │ Retry with │  │ Return Error │
                          │ new code   │  │ (extremely   │
                          └────────────┘  │  rare)       │
                                          └──────────────┘
```

### Service Layer Implementation

```go
type URLService struct {
    repo  Repository
    clock Clock
}

func (s *URLService) Create(ctx context.Context, longURL string, ttl time.Duration) (*URLRecord, error) {
    const maxRetries = 5

    for attempt := 0; attempt < maxRetries; attempt++ {
        code := generateRandomCode()

        record := &URLRecord{
            ShortCode:      code,
            LongURL:        longURL,
            CreatedAt:      s.clock.Now(),
            ExpiresAt:      s.clock.Now().Add(ttl),
            ClickCount:     0,
            LastAccessedAt: time.Time{}, // Zero value until first access
        }

        err := s.repo.SaveIfNotExists(ctx, record)
        if err == nil {
            return record, nil
        }

        if errors.Is(err, ErrCodeExists) {
            continue // Collision, retry with new code
        }

        return nil, fmt.Errorf("saving record: %w", err)
    }

    return nil, ErrMaxRetriesExceeded
}
```

### Repository Interface Contract

```go
type Repository interface {
    // SaveIfNotExists atomically saves the record only if the short code
    // doesn't already exist. Returns ErrCodeExists if the code is taken.
    // This operation MUST be atomic to prevent race conditions.
    SaveIfNotExists(ctx context.Context, record *URLRecord) error

    // FindByShortCode retrieves a record by its short code.
    // Returns ErrNotFound if the code doesn't exist.
    FindByShortCode(ctx context.Context, code string) (*URLRecord, error)

    // IncrementClickCount atomically increments the click counter
    // and updates LastAccessedAt. Thread-safe for concurrent access.
    IncrementClickCount(ctx context.Context, code string, accessTime time.Time) error

    // DeleteExpired removes all records where ExpiresAt < before.
    // Returns the number of deleted records.
    DeleteExpired(ctx context.Context, before time.Time) (int64, error)
}
```

## Collision Handling at Scale

### Scenario: 1.2 Billion URLs (12 months at 100M/month)

At this scale:
- **Per-insert collision probability:** ~33%
- **Probability of 5 consecutive collisions:** 0.33^5 = 0.4%
- **Probability of failure (all 5 retries collide):** 0.004%

For 100M inserts/month, expected failures: ~4,000/month = ~5/hour

**Mitigation strategies if this becomes problematic:**

1. **Increase code length to 9 characters**
   - New combinations: 34^9 = 60.7 trillion
   - Collision probability drops to ~0.002%

2. **Implement exponential backoff between retries**

3. **Add monitoring for retry metrics**

## Security Considerations

| Concern | Mitigation |
|---------|------------|
| Code enumeration | Random generation prevents sequential guessing |
| Code prediction | crypto/rand provides cryptographic randomness |
| URL content leakage | Code is not derived from URL content |
| Timing attacks | Constant-time comparison for code validation |

## Testing Short Code Generation

### Unit Test: Character Set Validation
```go
func TestGeneratedCodeExcludesAmbiguousChars(t *testing.T) {
    excluded := "0OIl1"
    for i := 0; i < 10000; i++ {
        code := generateRandomCode()
        for _, c := range code {
            if strings.ContainsRune(excluded, c) {
                t.Errorf("code %q contains excluded char %q", code, c)
            }
        }
    }
}
```

### Unit Test: Code Length
```go
func TestGeneratedCodeLength(t *testing.T) {
    for i := 0; i < 1000; i++ {
        code := generateRandomCode()
        if len(code) != 8 {
            t.Errorf("expected length 8, got %d", len(code))
        }
    }
}
```

### Integration Test: Collision Handling
```go
func TestCollisionRetry(t *testing.T) {
    repo := NewMockRepository()
    // Pre-populate with codes that will collide
    repo.ForceCollisions(3) // First 3 attempts will collide

    svc := NewURLService(repo, NewRealClock())
    record, err := svc.Create(ctx, "https://example.com", 24*time.Hour)

    assert.NoError(t, err)
    assert.NotEmpty(t, record.ShortCode)
    assert.Equal(t, 4, repo.SaveAttempts()) // 3 collisions + 1 success
}
```
