-- Dev ClickHouse seed data
-- Accessible from the app as connector: host=hnb-clickhouse, port=9000, database=analytics, user=dev, password=dev

CREATE DATABASE IF NOT EXISTS analytics;

-- E-commerce events table
CREATE TABLE IF NOT EXISTS analytics.events (
    event_id     UUID          DEFAULT generateUUIDv4(),
    event_type   LowCardinality(String),
    user_id      UInt64,
    session_id   String,
    page         String,
    country      LowCardinality(String),
    device       LowCardinality(String),
    revenue      Decimal(10, 2),
    ts           DateTime      DEFAULT now()
) ENGINE = MergeTree()
ORDER BY (event_type, ts);

INSERT INTO analytics.events (event_type, user_id, session_id, page, country, device, revenue, ts) VALUES
    ('page_view',  1001, 'sess-a1', '/home',      'US', 'desktop', 0.00, '2026-04-01 08:00:00'),
    ('page_view',  1001, 'sess-a1', '/pricing',   'US', 'desktop', 0.00, '2026-04-01 08:02:10'),
    ('signup',     1001, 'sess-a1', '/signup',     'US', 'desktop', 0.00, '2026-04-01 08:04:30'),
    ('page_view',  1002, 'sess-b2', '/home',       'BR', 'mobile',  0.00, '2026-04-01 09:15:00'),
    ('page_view',  1002, 'sess-b2', '/features',   'BR', 'mobile',  0.00, '2026-04-01 09:16:45'),
    ('purchase',   1002, 'sess-b2', '/checkout',   'BR', 'mobile',  49.00,'2026-04-01 09:20:00'),
    ('page_view',  1003, 'sess-c3', '/home',       'DE', 'desktop', 0.00, '2026-04-01 10:00:00'),
    ('purchase',   1003, 'sess-c3', '/checkout',   'DE', 'desktop', 99.00,'2026-04-01 10:05:00'),
    ('page_view',  1004, 'sess-d4', '/blog',       'GB', 'tablet',  0.00, '2026-04-02 11:00:00'),
    ('page_view',  1004, 'sess-d4', '/pricing',    'GB', 'tablet',  0.00, '2026-04-02 11:02:00'),
    ('signup',     1004, 'sess-d4', '/signup',     'GB', 'tablet',  0.00, '2026-04-02 11:05:00'),
    ('purchase',   1004, 'sess-d4', '/checkout',   'GB', 'tablet',  49.00,'2026-04-02 11:10:00'),
    ('page_view',  1005, 'sess-e5', '/home',       'US', 'mobile',  0.00, '2026-04-03 14:00:00'),
    ('page_view',  1005, 'sess-e5', '/features',   'US', 'mobile',  0.00, '2026-04-03 14:01:30'),
    ('page_view',  1006, 'sess-f6', '/home',       'JP', 'desktop', 0.00, '2026-04-03 15:00:00'),
    ('signup',     1006, 'sess-f6', '/signup',     'JP', 'desktop', 0.00, '2026-04-03 15:04:00'),
    ('purchase',   1006, 'sess-f6', '/checkout',   'JP', 'desktop', 199.00,'2026-04-03 15:08:00'),
    ('page_view',  1007, 'sess-g7', '/blog',       'CA', 'desktop', 0.00, '2026-04-04 09:00:00'),
    ('page_view',  1007, 'sess-g7', '/pricing',    'CA', 'desktop', 0.00, '2026-04-04 09:03:00'),
    ('purchase',   1007, 'sess-g7', '/checkout',   'CA', 'desktop', 99.00,'2026-04-04 09:07:00');

-- Daily revenue summary (materialized-style view for quick dashboards)
CREATE TABLE IF NOT EXISTS analytics.daily_revenue (
    date         Date,
    country      LowCardinality(String),
    orders       UInt32,
    revenue      Decimal(10, 2)
) ENGINE = MergeTree()
ORDER BY (date, country);

INSERT INTO analytics.daily_revenue VALUES
    ('2026-04-01', 'US',  0,    0.00),
    ('2026-04-01', 'BR',  1,   49.00),
    ('2026-04-01', 'DE',  1,   99.00),
    ('2026-04-02', 'GB',  1,   49.00),
    ('2026-04-03', 'US',  0,    0.00),
    ('2026-04-03', 'JP',  1,  199.00),
    ('2026-04-04', 'CA',  1,   99.00);

-- User table
CREATE TABLE IF NOT EXISTS analytics.users (
    user_id      UInt64,
    email        String,
    plan         LowCardinality(String),
    signed_up_at DateTime,
    country      LowCardinality(String)
) ENGINE = MergeTree()
ORDER BY user_id;

INSERT INTO analytics.users VALUES
    (1001, 'alice@example.com',   'free',  '2026-04-01 08:04:30', 'US'),
    (1002, 'bob@example.com',     'pro',   '2026-03-15 12:00:00', 'BR'),
    (1003, 'carol@example.com',   'pro',   '2026-02-20 09:00:00', 'DE'),
    (1004, 'dave@example.com',    'pro',   '2026-04-02 11:05:00', 'GB'),
    (1005, 'eve@example.com',     'free',  '2026-01-10 16:00:00', 'US'),
    (1006, 'frank@example.com',   'enterprise', '2026-04-03 15:04:00', 'JP'),
    (1007, 'grace@example.com',   'pro',   '2026-03-01 08:00:00', 'CA');
