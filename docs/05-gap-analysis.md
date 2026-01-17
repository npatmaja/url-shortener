# Gap Analysis: In-Memory vs. Managed Storage

## Problem Statement

The Terraform infrastructure defines a stateless serverless compute environment (AWS Lambda or GCP Cloud Run). This document explains why the in-memory storage implementation is fundamentally incompatible with this architecture and how managed storage (DynamoDB/Firestore) resolves these issues.

## Serverless Architecture Characteristics

### AWS Lambda / GCP Cloud Run Behavior

| Characteristic | Impact on In-Memory Storage |
|----------------|----------------------------|
| **Stateless instances** | Each invocation may run on a different container |
| **Cold starts** | New containers start with empty memory |
| **Horizontal scaling** | Multiple concurrent instances don't share memory |
| **Instance recycling** | Containers are terminated after idle timeout |
| **No persistent disk** | Container filesystem is ephemeral |

## Gap Analysis

### Gap 1: Data Loss on Cold Start

**Problem:**
```
Request 1 → Lambda Instance A (cold start)
           ↓
           Creates short URL "abc123" in memory
           ↓
           Instance A idle → terminated

Request 2 → Lambda Instance B (cold start)
           ↓
           Memory is empty
           ↓
           "abc123" not found → 404 Error
```

**Impact:** URLs become inaccessible randomly, defeating the purpose of the service.

**Solution with DynamoDB:**
```
Request 1 → Lambda Instance A
           ↓
           Writes to DynamoDB (persistent)
           ↓
           Instance A terminated

Request 2 → Lambda Instance B
           ↓
           Reads from DynamoDB
           ↓
           "abc123" found → Redirect works
```

---

### Gap 2: Data Inconsistency Across Instances

**Problem:**
```
                    ┌─────────────────┐
                    │   API Gateway   │
                    └────────┬────────┘
                             │
        ┌────────────────────┼────────────────────┐
        ▼                    ▼                    ▼
┌───────────────┐   ┌───────────────┐   ┌───────────────┐
│  Instance A   │   │  Instance B   │   │  Instance C   │
│ memory: {     │   │ memory: {     │   │ memory: {     │
│   "abc": url1 │   │   "def": url2 │   │   (empty)     │
│ }             │   │ }             │   │ }             │
└───────────────┘   └───────────────┘   └───────────────┘
```

**Impact:**
- URL created in Instance A not visible from Instance B
- Click counts diverge across instances
- Statistics are incorrect

**Solution with DynamoDB:**
All instances read/write to the same DynamoDB table → consistent view.

---

### Gap 3: Lost Click Counts

**Problem:**
```
Redirect Request 1 → Instance A → click_count = 1 (in memory)
Redirect Request 2 → Instance B → click_count = 1 (different memory)
Redirect Request 3 → Instance A → click_count = 2 (Instance A's memory)

Stats Request → Instance C → click_count = 0 (empty memory)
```

**Impact:** Click analytics are unreliable and incomplete.

**Solution with DynamoDB:**
Atomic `UpdateItem` with `ADD click_count 1` ensures accurate counting regardless of which instance handles the request.

---

### Gap 4: No Durability Guarantee

**Problem:**

| Event | In-Memory Impact |
|-------|------------------|
| Instance crash | All data lost |
| AWS zone failure | All affected instances lose data |
| Deployment | All instances replaced, data lost |
| Auto-scaling down | Terminated instances lose data |

**Impact:** Zero durability. Any infrastructure event causes complete data loss.

**Solution with DynamoDB:**
- 99.999999999% (11 9s) durability
- Automatic 3-way replication across Availability Zones
- Point-in-time recovery for disaster recovery

---

### Gap 5: No Horizontal Scalability

**Problem:**
```
Scale-out event: 1 instance → 10 instances

                    Request distribution:
                    ┌─────────────────────────────────┐
                    │ 10% of requests hit Instance A  │
                    │ (where the data was created)    │
                    │                                 │
                    │ 90% of requests hit new         │
                    │ instances → 404 errors          │
                    └─────────────────────────────────┘
```

**Impact:** The more you scale, the worse the user experience becomes.

**Solution with DynamoDB:**
All instances connect to the same table. Scaling instances doesn't affect data availability.

---

## Comparison Table

| Aspect | In-Memory | DynamoDB |
|--------|-----------|----------|
| **Data persistence** | Lost on restart | Permanent |
| **Cross-instance consistency** | None | Guaranteed |
| **Horizontal scaling** | Broken | Transparent |
| **Cold start handling** | Data unavailable | Data available |
| **Click count accuracy** | Inconsistent | Atomic counters |
| **Failure recovery** | No recovery | Automatic |
| **TTL expiration** | In-process only | Built-in TTL feature |

## Recommended Production Architecture

```
┌──────────────────────────────────────────────────────────────────────┐
│                         AWS Architecture                              │
├──────────────────────────────────────────────────────────────────────┤
│                                                                       │
│    ┌─────────────┐     ┌─────────────┐     ┌─────────────────────┐  │
│    │ CloudFront  │────▶│API Gateway  │────▶│  Lambda Function    │  │
│    │   (CDN)     │     │             │     │  (Stateless Go)     │  │
│    └─────────────┘     └─────────────┘     └──────────┬──────────┘  │
│                                                        │             │
│                                                        ▼             │
│                                            ┌─────────────────────┐  │
│                                            │     DynamoDB        │  │
│                                            │  - On-demand mode   │  │
│                                            │  - TTL enabled      │  │
│                                            │  - Point-in-time    │  │
│                                            │    recovery         │  │
│                                            └─────────────────────┘  │
│                                                                       │
└──────────────────────────────────────────────────────────────────────┘
```

## DynamoDB Repository Implementation

```go
package repository

import (
    "context"
    "time"

    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
    "github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
    "github.com/aws/aws-sdk-go-v2/service/dynamodb"
    "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

    "github.com/npatmaja/url-shortener/internal/domain"
)

type DynamoRepository struct {
    client    *dynamodb.Client
    tableName string
}

// SaveIfNotExists uses conditional write to prevent collisions.
func (r *DynamoRepository) SaveIfNotExists(ctx context.Context, record *domain.URLRecord) error {
    item, err := attributevalue.MarshalMap(toDynamoItem(record))
    if err != nil {
        return err
    }

    _, err = r.client.PutItem(ctx, &dynamodb.PutItemInput{
        TableName:           aws.String(r.tableName),
        Item:                item,
        ConditionExpression: aws.String("attribute_not_exists(short_code)"),
    })

    if err != nil {
        var ccf *types.ConditionalCheckFailedException
        if errors.As(err, &ccf) {
            return domain.ErrCodeExists
        }
        return err
    }

    return nil
}

// IncrementClickCount uses atomic counter update.
func (r *DynamoRepository) IncrementClickCount(ctx context.Context, code string, accessTime time.Time) error {
    update := expression.Set(
        expression.Name("click_count"),
        expression.Plus(expression.Name("click_count"), expression.Value(1)),
    ).Set(
        expression.Name("last_accessed_at"),
        expression.Value(accessTime.UnixMilli()),
    )

    expr, err := expression.NewBuilder().WithUpdate(update).Build()
    if err != nil {
        return err
    }

    _, err = r.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
        TableName: aws.String(r.tableName),
        Key: map[string]types.AttributeValue{
            "short_code": &types.AttributeValueMemberS{Value: code},
        },
        UpdateExpression:          expr.Update(),
        ExpressionAttributeNames:  expr.Names(),
        ExpressionAttributeValues: expr.Values(),
    })

    return err
}
```

## Migration Strategy

When moving from development (in-memory) to production (DynamoDB):

1. **Interface compatibility:** Both implementations satisfy the same `Repository` interface
2. **Zero code changes:** Business logic (`URLService`) remains unchanged
3. **Configuration-driven:** Switch via environment variable:
   ```go
   func NewRepository(cfg *Config) Repository {
       if cfg.UseInMemory {
           return NewMemoryRepository()
       }
       return NewDynamoRepository(cfg.DynamoClient, cfg.TableName)
   }
   ```

## Conclusion

In-memory storage is intentionally designed for:
- Local development
- Unit testing
- Integration testing

Production deployments **must** use managed storage (DynamoDB, Firestore, or Redis) to ensure data durability, consistency, and horizontal scalability in the serverless environment.
