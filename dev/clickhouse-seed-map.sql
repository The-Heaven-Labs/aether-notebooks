CREATE TABLE IF NOT EXISTS analytics.world_cities (
    city        String,
    country     String,
    lat         Float64,
    lon         Float64,
    population  UInt64,
    sales       Float64
) ENGINE = MergeTree()
ORDER BY (country, city);

INSERT INTO analytics.world_cities VALUES
    ('New York',     'USA',      40.7128,   -74.0060, 8336817,   12500000),
    ('Los Angeles',  'USA',      34.0522,   -118.2437, 3979576,   8900000),
    ('Chicago',      'USA',      41.8781,   -87.6298,  2693976,   5600000),
    ('Houston',      'USA',      29.7604,   -95.3698,  2320268,   4100000),
    ('London',       'UK',       51.5074,   -0.1278,   8982000,   15200000),
    ('Paris',        'France',   48.8566,    2.3522,   2161000,   9800000),
    ('Berlin',       'Germany',  52.5200,   13.4050,   3644826,   7200000),
    ('Madrid',       'Spain',    40.4168,   -3.7038,   3266126,   6100000),
    ('Rome',         'Italy',    41.9028,   12.4964,   2873000,   4800000),
    ('Tokyo',        'Japan',    35.6762,  139.6503,  13929286,   21000000),
    ('Shanghai',     'China',    31.2304,  121.4737,  24870895,   18500000),
    ('Beijing',      'China',    39.9042,  116.4074,  21542000,   16200000),
    ('Mumbai',       'India',    19.0760,   72.8777,  12478447,   9200000),
    ('Sydney',       'Australia',-33.8688,  151.2093,  5312163,   7800000),
    ('São Paulo',    'Brazil',  -23.5505,  -46.6333,  12325232,   8500000),
    ('Toronto',      'Canada',   43.6532,   -79.3832,  2731571,   5400000),
    ('Dubai',        'UAE',      25.2048,   55.2708,   3331400,   11200000),
    ('Singapore',    'Singapore', 1.3521,  103.8198,   5685807,   9600000),
    ('Seoul',        'Korea',    37.5665,  126.9780,   9776000,   13100000),
    ('Mexico City',  'Mexico',   19.4326,  -99.1332,  9209944,   6700000);

CREATE TABLE IF NOT EXISTS analytics.earthquake_data (
    location    String,
    region      String,
    magnitude   Float32,
    depth_km    UInt16,
    lat         Float64,
    lon         Float64
) ENGINE = MergeTree()
ORDER BY (region, location);

INSERT INTO analytics.earthquake_data VALUES
    ('Tohoku',        'Japan',        9.1,  29,  38.322,  142.369),
    ('Sumatra',       'Indonesia',    9.1,  30,   3.295,   95.982),
    ('Sendai',        'Japan',        8.2,  44,  38.104,  144.554),
    ('Iquique',       'Chile',        8.2,  20, -19.610,  -70.769),
    ('Maule',         'Chile',        8.8,  23, -35.909,  -72.733),
    ('San Francisco', 'USA',          7.8,  10,  37.775, -122.418),
    ('Kashmir',       'Pakistan',     7.6,  26,  34.494,   73.629),
    ('Port-au-Prince','Haiti',        7.0,  13,  18.443,  -72.571),
    ('Christchurch',  'New Zealand',  6.3,   5, -43.583,  172.680),
    ('Reykjavik',     'Iceland',      6.5,  10,  64.146,  -21.942),
    ('Nepal',         'Nepal',        7.8,  15,  28.147,   84.708),
    ('Kobe',          'Japan',        6.9,  16,  34.679,  135.058),
    ('Izmit',         'Turkey',       7.6,  17,  40.700,   29.987),
    ('Bam',           'Iran',         6.6,  10,  29.009,   58.337),
    ('Loma Prieta',   'USA',          6.9,  18,  37.040, -121.880);
