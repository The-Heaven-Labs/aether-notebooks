-- Linux process tree dataset for testing Tree chart visualizations
-- Shows process hierarchy via PID / PPID columns

CREATE TABLE IF NOT EXISTS analytics.linux_processes (
    timestamp     DateTime,
    pid           UInt32,
    ppid          UInt32,
    process_name  String,
    user_name     LowCardinality(String),
    cpu_percent   Float32,
    memory_mb     Float32,
    status        LowCardinality(String),
    command       String
) ENGINE = MergeTree()
ORDER BY (timestamp, pid);

-- Level 0: Kernel / init
-- Level 1: Core system services (children of systemd)
-- Level 2: SSH and Docker sub-processes
-- Level 3: User login shells + Docker containers
-- Level 4: Nginx workers, postgres backends, user commands
-- Level 5: Python sub-processes, node sub-processes
-- Level 6: Worker pool processes
-- Zombie processes (status Z) for edge case testing

INSERT INTO analytics.linux_processes (timestamp, pid, ppid, process_name, user_name, cpu_percent, memory_mb, status, command) VALUES
('2026-06-14 08:00:00', 1,    0,    'systemd',        'root',     0.1,  12.4,  'S', '/sbin/init'),
('2026-06-14 08:00:00', 2,    0,    'kthreadd',       'root',     0.0,   0.0,  'S', '[kthreadd]'),
('2026-06-14 08:00:00', 500,  1,    'rsyslogd',       'root',     0.2,   5.1,  'S', '/usr/sbin/rsyslogd -n'),
('2026-06-14 08:00:00', 510,  1,    'cron',           'root',     0.0,   2.3,  'S', '/usr/sbin/cron -f'),
('2026-06-14 08:00:00', 520,  1,    'dbus-daemon',    'messagebus', 0.3, 3.8,  'S', '/usr/bin/dbus-daemon --system'),
('2026-06-14 08:00:00', 530,  1,    'networkd',       'root',     0.4,   8.2,  'S', '/lib/systemd/systemd-networkd'),
('2026-06-14 08:00:00', 540,  1,    'resolved',       'systemd-resolve', 0.2, 12.6, 'S', '/lib/systemd/systemd-resolved'),
('2026-06-14 08:00:00', 550,  1,    'journal',        'systemd-journal', 0.1, 28.4, 'S', '/lib/systemd/systemd-journald'),
('2026-06-14 08:00:00', 560,  1,    'udevd',          'root',     0.1,   3.2,  'S', '/lib/systemd/udevd'),
('2026-06-14 08:00:00', 570,  1,    'login',          'root',     0.0,   1.8,  'S', '/usr/bin/login --'),
('2026-06-14 08:00:00', 580,  1,    'containerd',     'root',     1.2,  45.3,  'S', '/usr/bin/containerd'),
('2026-06-14 08:00:00', 590,  1,    'dockerd',        'root',     2.1,  89.7,  'S', '/usr/bin/dockerd -H fd://'),
('2026-06-14 08:00:00', 600,  1,    'sshd',           'root',     0.1,   4.5,  'S', '/usr/sbin/sshd -D'),
('2026-06-14 08:00:00', 605,  600,  'sshd',           'root',     0.1,   3.9,  'S', 'sshd: jesus [priv]'),
('2026-06-14 08:00:00', 610,  580,  'containerd-shim','root',     0.8,  15.2,  'S', 'containerd-shim-runc-v2 -namespace moby'),
('2026-06-14 08:00:00', 620,  590,  'docker-proxy',   'root',     0.3,   8.4,  'S', 'docker-proxy -proto tcp -host-ip 0.0.0.0'),
('2026-06-14 08:00:00', 630,  590,  'containerd-shim','root',     0.9,  14.8,  'S', 'containerd-shim-runc-v2 -namespace moby'),
('2026-06-14 08:05:00', 700,  605,  'sshd',           'jesus',    0.0,   2.1,  'S', 'sshd: jesus@pts/0'),
('2026-06-14 08:05:00', 710,  700,  'bash',           'jesus',    0.1,   3.4,  'S', '-bash'),
('2026-06-14 08:05:00', 720,  605,  'sshd',           'jesus',    0.0,   2.0,  'S', 'sshd: jesus@pts/1'),
('2026-06-14 08:05:00', 730,  720,  'bash',           'jesus',    0.1,   3.2,  'S', '-bash'),
('2026-06-14 08:00:00', 800,  610,  'nginx',          'root',     0.5,  12.3,  'S', 'nginx: master process nginx -g daemon off;'),
('2026-06-14 08:00:00', 810,  630,  'redis-server',   'redis',    0.3,  24.8,  'S', 'redis-server *:6379'),
('2026-06-14 08:00:00', 820,  630,  'postgres',       'postgres', 1.8,  68.4,  'S', 'postgres: startup, recovering shared memory'),
('2026-06-14 08:00:00', 801,  800,  'nginx',          'www-data',  0.4,   8.1,  'S', 'nginx: worker process'),
('2026-06-14 08:00:00', 802,  800,  'nginx',          'www-data',  0.3,   7.9,  'S', 'nginx: worker process'),
('2026-06-14 08:00:00', 803,  800,  'nginx',          'www-data',  0.2,   7.6,  'S', 'nginx: worker process'),
('2026-06-14 08:00:00', 804,  800,  'nginx',          'www-data',  0.1,   7.5,  'S', 'nginx: cache manager process'),
('2026-06-14 08:00:00', 821,  820,  'postgres',       'postgres',  0.1,   6.8,  'S', 'postgres: background writer'),
('2026-06-14 08:00:00', 822,  820,  'postgres',       'postgres',  0.1,   4.2,  'S', 'postgres: checkpointer'),
('2026-06-14 08:00:00', 823,  820,  'postgres',       'postgres',  0.0,   5.1,  'S', 'postgres: WAL writer'),
('2026-06-14 08:00:00', 824,  820,  'postgres',       'postgres',  0.1,   3.8,  'S', 'postgres: stats collector'),
('2026-06-14 08:05:00', 825,  820,  'postgres',       'postgres',  0.8,  15.2,  'S', 'postgres: jesus analytics 192.168.1.50 idle'),
('2026-06-14 08:10:00', 900,  710,  'python3',        'jesus',    3.2,  85.4,  'S', 'python3 manage.py runserver'),
('2026-06-14 08:10:00', 910,  710,  'node',           'jesus',    4.5, 142.3,  'S', 'node /app/node_modules/.bin/vite'),
('2026-06-14 08:10:00', 920,  710,  'docker',         'jesus',    0.1,   4.2,  'S', 'docker compose up -d'),
('2026-06-14 08:15:00', 930,  730,  'vim',            'jesus',    0.2,  12.8,  'S', 'vim /home/jesus/projects/api.py'),
('2026-06-14 08:15:00', 940,  730,  'htop',           'jesus',    1.8,   8.4,  'S', 'htop'),
('2026-06-14 08:12:00', 950,  900,  'celery',         'jesus',    8.4,  64.2,  'S', 'celery -A tasks worker --loglevel=info'),
('2026-06-14 08:12:00', 951,  900,  'celery-beat',    'jesus',    0.5,  22.1,  'S', 'celery -A tasks beat'),
('2026-06-14 08:12:00', 960,  910,  'esbuild',        'jesus',    2.1,  48.6,  'S', 'esbuild --bundle --outfile=dist/bundle.js'),
('2026-06-14 08:12:00', 961,  910,  'tsc',            'jesus',    1.4,  96.3,  'S', 'tsc --noEmit --watch'),
('2026-06-14 08:12:00', 970,  920,  'docker-compose', 'jesus',    0.8,  32.4,  'S', 'docker-compose -f docker-compose.dev.yml up'),
('2026-06-14 08:12:00', 952,  950,  'celery-worker',  'jesus',    2.4,  58.6,  'S', 'celery worker child'),
('2026-06-14 08:12:00', 953,  950,  'celery-worker',  'jesus',    1.9,  54.2,  'S', 'celery worker child'),
('2026-06-14 08:12:00', 954,  950,  'celery-worker',  'jesus',    2.1,  56.8,  'S', 'celery worker child'),
('2026-06-14 09:00:00', 1000,  510,  'cron',           'root',     0.0,   1.2,  'S', '(CRON)'),
('2026-06-14 09:00:00', 1001, 1000,  'run-parts',      'root',     0.1,   0.8,  'S', 'run-parts /etc/cron.hourly'),
('2026-06-14 09:00:00', 1002, 1001,  'logrotate',      'root',     0.2,   3.4,  'S', '/usr/sbin/logrotate /etc/logrotate.conf'),
('2026-06-14 09:00:00', 1003, 1001,  'backup.sh',      'root',     1.5,   6.8,  'S', '/usr/local/bin/backup.sh --incremental'),
('2026-06-14 08:00:00', 1100,  1,    'node_exporter',  'root',     0.4,  18.2,  'S', '/usr/local/bin/node_exporter'),
('2026-06-14 08:00:00', 1110,  1,    'prometheus',     'root',     1.2,  95.4,  'S', '/usr/local/bin/prometheus --config.file=/etc/prometheus/prometheus.yml'),
('2026-06-14 08:00:00', 1120,  1,    'grafana',        'grafana',  0.8,  42.6,  'S', '/usr/sbin/grafana-server --homepath=/usr/share/grafana'),
('2026-06-14 08:30:00', 1200,  952,  'celery-worker',  'jesus',    0.0,   0.0,  'Z', 'celery worker child <defunct>'),
('2026-06-14 08:35:00', 1300, 1003,  'tar',            'root',     0.0,   0.0,  'Z', 'tar czf /backup/daily.tar.gz <defunct>');
