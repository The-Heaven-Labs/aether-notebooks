CREATE TABLE IF NOT EXISTS analytics.website_traffic (
    source_page    String,
    target_page    String,
    sessions       UInt64
) ENGINE = MergeTree()
ORDER BY (source_page, target_page);

INSERT INTO analytics.website_traffic VALUES
    ('Homepage',            'Products',      45000),
    ('Homepage',            'Pricing',       22000),
    ('Homepage',            'Blog',          18000),
    ('Homepage',            'Sign Up',       12000),
    ('Homepage',            'About',          5000),
    ('Products',            'Product A',     28000),
    ('Products',            'Product B',     15000),
    ('Products',            'Product C',      8000),
    ('Product A',           'Pricing',       14000),
    ('Product A',           'Sign Up',       10000),
    ('Product A',           'Documentation',  6000),
    ('Product B',           'Pricing',        8000),
    ('Product B',           'Sign Up',        5000),
    ('Product B',           'Documentation',  4000),
    ('Product C',           'Pricing',        4000),
    ('Product C',           'Sign Up',        2000),
    ('Pricing',             'Sign Up',       18000),
    ('Pricing',             'Homepage',       4000),
    ('Blog',                'Products',       8000),
    ('Blog',                'Homepage',       6000),
    ('Blog',                'About',          3000),
    ('Documentation',       'Sign Up',        5000),
    ('Documentation',       'Products',       3000),
    ('Documentation',       'API Reference',  7000),
    ('API Reference',       'Sign Up',        5000),
    ('About',               'Homepage',       2000),
    ('About',               'Careers',        3000),
    ('Careers',             'Homepage',       1500);

CREATE TABLE IF NOT EXISTS analytics.supply_chain (
    source      String,
    target      String,
    volume_tons UInt64
) ENGINE = MergeTree()
ORDER BY (source, target);

INSERT INTO analytics.supply_chain VALUES
    ('Raw Materials',   'Manufacturing',    85000),
    ('Raw Materials',   'Packaging',        15000),
    ('Manufacturing',   'Distribution',     62000),
    ('Manufacturing',   'Quality Control',  18000),
    ('Manufacturing',   'Warehouse',         5000),
    ('Quality Control', 'Distribution',     16000),
    ('Quality Control', 'Rework',            2000),
    ('Distribution',    'Retail',           48000),
    ('Distribution',    'Wholesale',        25000),
    ('Distribution',    'Warehouse',         5000),
    ('Wholesale',       'Retail',           20000),
    ('Wholesale',       'Export',            5000),
    ('Warehouse',       'Retail',            8000),
    ('Warehouse',       'Wholesale',         2000),
    ('Rework',          'Distribution',      2000),
    ('Retail',          'Customers',        68000),
    ('Export',          'Customers',         5000);
