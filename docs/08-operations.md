# Operations, SLIs, SLOs, and Capacity Planning

## Service Level Management

### Service Level Indicators (SLIs)

| SLI | Definition | Measurement Method |
|-----|------------|-------------------|
| **Redirect Latency** | Time from request received to 302 response sent | P99 of `X-Processing-Time-Micros` for `/s/{code}` endpoint |
| **Availability** | Percentage of successful responses (non-5xx) | `(total_requests - 5xx_responses) / total_requests * 100` |

### Service Level Objectives (SLOs)

| SLI | SLO Target | Rationale |
|-----|------------|-----------|
| **Redirect Latency (P99)** | < 100ms | Redirects should be imperceptible to users. P99 ensures consistent experience |
| **Availability** | 99.9% (three nines) | ~8.7 hours downtime/year acceptable for non-critical URL shortener |

### Error Budget

With 99.9% availability SLO:
- **Monthly error budget:** 43.2 minutes of downtime
- **Weekly error budget:** ~10 minutes of downtime

### Alerting Thresholds

| Metric | Warning | Critical | Action |
|--------|---------|----------|--------|
| Redirect P99 latency | > 80ms | > 150ms | Page on-call |
| Error rate (5xx) | > 0.5% | > 1% | Page on-call |
| DynamoDB throttling | Any | Sustained | Investigate capacity |

---

## On-Call Intervention Scenarios

### Scenario: DynamoDB Throttling During Traffic Spike

**Symptoms:**
- Elevated 5xx error rate (> 1%)
- DynamoDB `ThrottledRequests` metric spiking
- Latency P99 exceeds 500ms

**Root Cause:**
With on-demand capacity, DynamoDB scales automatically but may throttle during sudden traffic spikes (> 2x in 30 minutes).

**Immediate Actions:**
1. Check CloudWatch for `ConsumedReadCapacityUnits` and `ConsumedWriteCapacityUnits`
2. If sustained, consider switching to provisioned capacity temporarily
3. Enable DynamoDB auto-scaling with higher max capacity

**Post-Incident:**
1. Review traffic patterns
2. Consider pre-warming during expected spikes
3. Implement request queuing/circuit breaker

---

## Capacity Planning

### Storage Requirements (12 Months at 100M URLs/Month)

**Per-Record Storage:**

| Field | Size (bytes) | Notes |
|-------|--------------|-------|
| short_code | 8 | 8 characters |
| long_url | 100 avg | Variable, capped at 2048 |
| created_at | 8 | Unix timestamp |
| expires_at | 8 | Unix timestamp |
| click_count | 8 | int64 |
| last_accessed_at | 8 | Unix timestamp |
| DynamoDB overhead | ~100 | Index, metadata |

**Estimated size per record: ~240 bytes**

**12-Month Projection:**

| Month | Cumulative URLs | Storage (Raw) | With Indexes |
|-------|-----------------|---------------|--------------|
| 1 | 100M | 24 GB | 36 GB |
| 3 | 300M | 72 GB | 108 GB |
| 6 | 600M | 144 GB | 216 GB |
| 12 | 1.2B | 288 GB | 432 GB |

**Assumptions:**
- Default 24h TTL means most URLs expire
- Actual storage depends on TTL distribution
- If average TTL is 24h, active storage is ~100M records (~24 GB) at steady state

**With TTL Expiration (Realistic):**

| Scenario | Active Records | Storage |
|----------|---------------|---------|
| All URLs 24h TTL | ~100M | 24 GB |
| 50% 24h, 50% 7d | ~450M | 108 GB |
| 20% 24h, 80% 30d | ~2.4B | 576 GB |

### Redirect Endpoint Scaling (10,000 RPS)

**Single Lambda Execution:**
- Cold start: ~200-500ms (Go, small binary)
- Warm execution: 5-20ms
- DynamoDB read latency: 1-5ms (single-digit ms SLA)
- **Total warm latency: ~10-30ms**

**Concurrency Calculation:**
```
Required concurrency = RPS * average_duration
                     = 10,000 * 0.025s
                     = 250 concurrent executions
```

**AWS Lambda Limits:**
- Default concurrent executions: 1,000/account/region
- Can request increase to 10,000+

**Scaling Strategy:**

```
┌─────────────────────────────────────────────────────────────────────────┐
│                        10,000 RPS Architecture                           │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│   ┌─────────────┐     ┌─────────────────┐     ┌─────────────────────┐  │
│   │ CloudFront  │     │  API Gateway    │     │   Lambda (250+      │  │
│   │ (Caching    │────▶│  (Rate limiting │────▶│   concurrent)       │  │
│   │  302s)      │     │   10K RPS)      │     │                     │  │
│   └─────────────┘     └─────────────────┘     └──────────┬──────────┘  │
│                                                           │             │
│                              ┌────────────────────────────┘             │
│                              ▼                                          │
│                   ┌─────────────────────────┐                          │
│                   │  DynamoDB               │                          │
│                   │  On-demand scaling or   │                          │
│                   │  Provisioned:           │                          │
│                   │  - 10K RCU (reads)      │                          │
│                   │  - 1K WCU (writes)      │                          │
│                   └─────────────────────────┘                          │
│                                                                          │
│   Optional: DAX (DynamoDB Accelerator)                                  │
│   - Microsecond latency                                                 │
│   - Reduces DynamoDB reads by 10x                                       │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```

**Recommendations for 10K RPS:**

1. **Enable CloudFront caching for redirects**
   - Cache 302 responses for short TTL (1-5 seconds)
   - Reduces Lambda invocations by 50-90% for popular URLs

2. **DynamoDB DAX**
   - Adds in-memory caching layer
   - Microsecond read latency
   - Reduces read costs significantly

3. **Provisioned capacity with auto-scaling**
   - More predictable than on-demand for sustained load
   - Set auto-scaling to handle 2x expected traffic

4. **Request Lambda concurrency increase**
   - Request 500-1000 concurrent executions
   - Account-level limit, plan ahead

---

## Expiration Management: Hybrid Strategy

### Implementation

The hybrid approach combines lazy deletion (on access) with background cleanup.

```go
// Lazy deletion: Check expiration on every access
func (s *URLService) Resolve(ctx context.Context, code string) (string, error) {
    record, err := s.repo.FindByShortCode(ctx, code)
    if err != nil {
        return "", err
    }

    // Lazy expiration check
    if record.IsExpired(s.clock.Now()) {
        return "", domain.ErrExpired
    }

    // Increment click count (async to not block redirect)
    go func() {
        _ = s.repo.IncrementClickCount(context.Background(), code, s.clock.Now())
    }()

    return record.LongURL, nil
}
```

```go
// Background cleanup: Periodic sweep
type ExpirationService struct {
    repo     Repository
    clock    Clock
    interval time.Duration
}

func (s *ExpirationService) Start(ctx context.Context) {
    ticker := time.NewTicker(s.interval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            deleted, err := s.repo.DeleteExpired(ctx, s.clock.Now())
            if err != nil {
                slog.Error("expiration cleanup failed", "error", err)
                continue
            }
            if deleted > 0 {
                slog.Info("expired records deleted", "count", deleted)
            }
        }
    }
}
```

### Configuration

| Setting | Value | Rationale |
|---------|-------|-----------|
| Cleanup interval | 1 hour | Balance between freshness and cost |
| Batch size | 1000 | Avoid long-running transactions |
| DynamoDB TTL | Enabled | Automatic cleanup as backup |

### Trade-offs

| Strategy | Pros | Cons |
|----------|------|------|
| **Lazy only** | Simple, no background process | Expired data persists, storage waste |
| **Background only** | Clean storage | Expired URLs may work briefly |
| **Hybrid (chosen)** | Best of both | Slightly more complex |

With DynamoDB TTL enabled, even if background cleanup fails, DynamoDB will eventually delete expired items automatically.

---

## Monitoring Dashboard

### Key Metrics to Display

```
┌─────────────────────────────────────────────────────────────────────────┐
│                      URL Shortener Dashboard                             │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  ┌──────────────────────┐  ┌──────────────────────┐                     │
│  │ Redirect Latency P99 │  │ Availability (24h)   │                     │
│  │      ████ 45ms       │  │      ████ 99.97%     │                     │
│  │  Target: <100ms      │  │  Target: 99.9%       │                     │
│  └──────────────────────┘  └──────────────────────┘                     │
│                                                                          │
│  ┌──────────────────────┐  ┌──────────────────────┐                     │
│  │ Requests/sec         │  │ Error Rate           │                     │
│  │      ████ 2,450      │  │      ████ 0.02%      │                     │
│  └──────────────────────┘  └──────────────────────┘                     │
│                                                                          │
│  ┌─────────────────────────────────────────────────────────────────┐   │
│  │ Request Latency Distribution                                     │   │
│  │ P50: 12ms | P90: 35ms | P99: 45ms | P99.9: 120ms               │   │
│  └─────────────────────────────────────────────────────────────────┘   │
│                                                                          │
│  ┌─────────────────────────────────────────────────────────────────┐   │
│  │ DynamoDB                                                         │   │
│  │ Read Units: 850/1000 | Write Units: 120/500 | Throttled: 0     │   │
│  └─────────────────────────────────────────────────────────────────┘   │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```

### CloudWatch Alarms (AWS)

```hcl
resource "aws_cloudwatch_metric_alarm" "high_latency" {
  alarm_name          = "${local.name_prefix}-high-latency"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 3
  metric_name         = "Duration"
  namespace           = "AWS/Lambda"
  period              = 60
  statistic           = "p99"
  threshold           = 100
  alarm_description   = "Redirect latency P99 exceeds 100ms"

  dimensions = {
    FunctionName = aws_lambda_function.api.function_name
  }

  alarm_actions = [aws_sns_topic.alerts.arn]
}

resource "aws_cloudwatch_metric_alarm" "high_errors" {
  alarm_name          = "${local.name_prefix}-high-errors"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 2
  metric_name         = "5XXError"
  namespace           = "AWS/ApiGateway"
  period              = 60
  statistic           = "Sum"
  threshold           = 10
  alarm_description   = "More than 10 5xx errors in 1 minute"

  dimensions = {
    ApiId = aws_apigatewayv2_api.api.id
  }

  alarm_actions = [aws_sns_topic.alerts.arn]
}
```

---

## Future Enhancements

### Priority 1: Performance
- **DAX integration** - Microsecond reads for hot URLs
- **CloudFront caching** - Edge caching for popular redirects
- **Connection pooling** - Reuse DynamoDB connections

### Priority 2: Features
- **Custom short codes** - Allow users to specify their own codes
- **Analytics dashboard** - Click analytics with time series
- **QR code generation** - Generate QR codes for short URLs

### Priority 3: Security
- **Rate limiting per IP** - Prevent abuse (with privacy considerations)
- **URL scanning** - Check long URLs against malware databases
- **CAPTCHA for creation** - Prevent automated abuse

### Priority 4: Observability
- **Distributed tracing** - X-Ray/Jaeger integration
- **Custom metrics** - Business metrics (URLs created, redirects by region)
- **Log aggregation** - Centralized logging with structured queries
