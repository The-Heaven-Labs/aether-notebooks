-- AWS CloudTrail events table
-- Loaded from ~/Downloads/flaws_cloudtrail_logs/ by the cloudtrail-loader service

CREATE TABLE IF NOT EXISTS analytics.cloudtrail_events (
    event_time            DateTime,
    event_source          LowCardinality(String),
    event_name            LowCardinality(String),
    event_type            LowCardinality(String),
    aws_region            LowCardinality(String),
    source_ip             String,
    user_agent            String,
    user_type             LowCardinality(String),
    user_arn              String,
    user_account_id       String,
    access_key_id         String DEFAULT '',
    error_code            String DEFAULT '',
    error_message         String DEFAULT '',
    event_id              String,
    recipient_account_id  String,
    request_id            String,
    api_version           String DEFAULT '',
    read_only             Nullable(UInt8),
    request_params        String DEFAULT '',
    response_elements     String DEFAULT ''
) ENGINE = MergeTree()
ORDER BY (event_source, event_name, event_time);
