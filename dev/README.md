# Development Data

This directory contains seed data and configuration for development services.

## ClickHouse

The dev stack includes ClickHouse for analytics workloads. Two datasets are seeded on first startup:

### E-commerce Events (`analytics.events`)

Sample e-commerce event data for testing dashboards and queries. See `clickhouse-seed.sql`.

### AWS CloudTrail Logs (`analytics.cloudtrail_events`)

Sample CloudTrail data from the [flaws.cloud](https://flaws.cloud/) CTF challenge, useful for security analysis and log exploration.

**Table:** `analytics.cloudtrail_events`

**Schema:**
| Column | Type | Description |
|---|---|---|
| `event_time` | DateTime | When the API call occurred |
| `event_source` | LowCardinality(String) | AWS service (e.g., `s3.amazonaws.com`) |
| `event_name` | LowCardinality(String) | API action (e.g., `ListBuckets`) |
| `event_type` | LowCardinality(String) | Event type (e.g., `AwsApiCall`) |
| `aws_region` | LowCardinality(String) | AWS region |
| `source_ip` | String | Source IP address |
| `user_agent` | String | Client user agent |
| `user_type` | LowCardinality(String) | IAM identity type (Root, IAMUser, AssumedRole) |
| `user_arn` | String | ARN of the caller |
| `user_account_id` | String | AWS account ID |
| `access_key_id` | String | Access key used |
| `error_code` | String | Error code if failed (empty on success) |
| `error_message` | String | Error message if failed |
| `event_id` | String | Unique CloudTrail event ID |
| `recipient_account_id` | String | Account that received the request |
| `request_id` | String | Request ID |
| `api_version` | String | API version |
| `read_only` | Nullable(UInt8) | Whether the call was read-only |
| `request_params` | String | Request parameters (JSON) |
| `response_elements` | String | Response elements (JSON) |

**Sample data:** 1,100 records across 3 files in `cloudtrail-logs/` (~1.3 MB)
- Date range: 2017-02-12 → 2020-10-07
- 139 distinct AWS API events across 47 services
- ~80% error records for testing failure analysis

**Loader:** `scripts/load-cloudtrail.py` runs automatically via the `cloudtrail-loader` service on `docker compose up`. It:
1. Waits for ClickHouse to be healthy
2. Skips if data already exists (idempotent)
3. Parses JSON files, flattens nested fields, batch-inserts into ClickHouse

To reload fresh data:
```bash
# Drop the table
docker compose -f docker-compose.dev.yml exec aether-clickhouse clickhouse-client \
  --user dev --password dev --database analytics \
  --query "DROP TABLE IF EXISTS cloudtrail_events"

# Recreate table
docker compose -f docker-compose.dev.yml exec aether-clickhouse clickhouse-client \
  --user dev --password dev --database analytics \
  --query "$(cat dev/clickhouse-seed-cloudtrail.sql)"

# Re-run loader
docker compose -f docker-compose.dev.yml up -d --force-recreate cloudtrail-loader
```

**Connection:** Use the existing ClickHouse connector settings:
- Host: `aether-clickhouse`
- Port: `9000` (native) / `8123` (HTTP)
- Database: `analytics`
- User: `dev`
- Password: `dev`

**Example queries:**
```sql
-- Top API calls
SELECT event_source, event_name, count() AS cnt
FROM cloudtrail_events
GROUP BY event_source, event_name
ORDER BY cnt DESC LIMIT 20;

-- Error rate by service
SELECT event_source,
       countIf(error_code != '') AS errors,
       count() AS total,
       round(errors / total * 100, 2) AS error_pct
FROM cloudtrail_events
GROUP BY event_source
ORDER BY total DESC;

-- Timeline of activity
SELECT toStartOfDay(event_time) AS hour, count() AS events
FROM cloudtrail_events
GROUP BY hour
ORDER BY hour;
```
