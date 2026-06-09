#!/usr/bin/env python3
"""
Load AWS CloudTrail JSON logs into ClickHouse for local development.

Reads CloudTrail files from /logs/ (bind-mounted from the host),
extracts individual records, flattens nested fields, and inserts
them into the analytics.cloudtrail_events table via ClickHouse HTTP API.

Environment:
    CLICKHOUSE_HOST     ClickHouse HTTP host (default: hnb-clickhouse)
    CLICKHOUSE_PORT     ClickHouse HTTP port (default: 8123)
    CLICKHOUSE_DB       Database name (default: analytics)
    CLICKHOUSE_USER     Username (default: dev)
    CLICKHOUSE_PASSWORD Password (default: dev)
    LOGS_DIR            Directory with CloudTrail JSON files (default: /logs)
    MAX_FILES           Max number of files to process (default: all)
    BATCH_SIZE          Records per INSERT request (default: 50000)
"""

import json
import os
import sys
import time
import urllib.request
import urllib.error
import urllib.parse
from pathlib import Path


def get_env(key: str, default: str) -> str:
    return os.environ.get(key, default)


def clickhouse_insert(host: str, port: str, db: str, user: str, password: str, query: str, data: str, max_retries: int = 5) -> None:
    """Send an INSERT query with JSONEachRow data to ClickHouse with retry."""
    url = f"http://{host}:{port}/?database={db}&user={user}&password={urllib.parse.quote(password)}"
    payload = f"{query}\n{data}".encode("utf-8")
    for attempt in range(1, max_retries + 1):
        try:
            req = urllib.request.Request(url, data=payload, method="POST")
            req.add_header("Content-Type", "application/x-www-form-urlencoded")
            with urllib.request.urlopen(req, timeout=300) as resp:
                resp.read()
            return
        except (ConnectionResetError, urllib.error.URLError) as e:
            if attempt == max_retries:
                raise
            wait = 2 ** attempt
            print(f"    Retry {attempt}/{max_retries} after error: {e}. Waiting {wait}s...")
            time.sleep(wait)


def parse_event_time(event_time: str) -> str:
    """Convert CloudTrail ISO timestamp (2017-02-12T19:57:06Z) to ClickHouse DateTime format (2017-02-12 19:57:06)."""
    if not event_time:
        return ""
    # Strip trailing Z and replace T with space
    return event_time.replace("T", " ").rstrip("Z")


def flatten_record(record: dict) -> dict:
    """Flatten a CloudTrail record into a flat dict for ClickHouse."""
    ui = record.get("userIdentity") or {}
    sc = ui.get("sessionContext") or {}
    attrs = sc.get("attributes") or {}

    # Serialize complex nested fields to JSON strings
    request_params = record.get("requestParameters")
    response_elems = record.get("responseElements")

    read_only = record.get("readOnly")
    if read_only is not None:
        read_only = 1 if str(read_only).lower() == "true" else 0

    return {
        "event_time": parse_event_time(record.get("eventTime", "")),
        "event_source": record.get("eventSource", ""),
        "event_name": record.get("eventName", ""),
        "event_type": record.get("eventType", ""),
        "aws_region": record.get("awsRegion", ""),
        "source_ip": record.get("sourceIPAddress", ""),
        "user_agent": record.get("userAgent", ""),
        "user_type": ui.get("type", ""),
        "user_arn": ui.get("arn", ""),
        "user_account_id": ui.get("accountId", ""),
        "access_key_id": ui.get("accessKeyId", ""),
        "error_code": record.get("errorCode", ""),
        "error_message": record.get("errorMessage", ""),
        "event_id": record.get("eventID", ""),
        "recipient_account_id": record.get("recipientAccountId", ""),
        "request_id": record.get("requestID", ""),
        "api_version": record.get("apiVersion", ""),
        "read_only": read_only,
        "request_params": json.dumps(request_params) if request_params else "",
        "response_elements": json.dumps(response_elems) if response_elems else "",
    }


def record_to_json_each_row(flat: dict) -> str:
    """Convert a flat record dict to a JSONEachRow line."""
    return json.dumps(flat, ensure_ascii=False)


def wait_for_clickhouse(host: str, port: str, user: str, password: str, max_retries: int = 30) -> bool:
    """Wait for ClickHouse to be ready."""
    url = f"http://{host}:{port}/ping"
    for i in range(max_retries):
        try:
            req = urllib.request.Request(url)
            with urllib.request.urlopen(req, timeout=3) as resp:
                if resp.status == 200:
                    print(f"ClickHouse is ready (attempt {i + 1})")
                    return True
        except Exception:
            pass
        print(f"Waiting for ClickHouse... (attempt {i + 1}/{max_retries})")
        time.sleep(2)
    return False


def main():
    ch_host = get_env("CLICKHOUSE_HOST", "hnb-clickhouse")
    ch_port = get_env("CLICKHOUSE_PORT", "8123")
    ch_db = get_env("CLICKHOUSE_DB", "analytics")
    ch_user = get_env("CLICKHOUSE_USER", "dev")
    ch_password = get_env("CLICKHOUSE_PASSWORD", "dev")
    logs_dir = get_env("LOGS_DIR", "/logs")
    max_files = int(get_env("MAX_FILES", "0"))  # 0 = all
    batch_size = int(get_env("BATCH_SIZE", "50000"))

    # Wait for ClickHouse
    if not wait_for_clickhouse(ch_host, ch_port, ch_user, ch_password):
        print("ERROR: ClickHouse is not available after retries", file=sys.stderr)
        sys.exit(1)

    # Find CloudTrail log files
    logs_path = Path(logs_dir)
    if not logs_path.exists():
        print(f"ERROR: Logs directory {logs_dir} does not exist", file=sys.stderr)
        sys.exit(1)

    files = sorted(logs_path.glob("*.json"))
    if not files:
        print(f"WARNING: No .json files found in {logs_dir}")
        sys.exit(0)

    if max_files > 0:
        files = files[:max_files]

    print(f"Found {len(files)} CloudTrail file(s) to process")

    # Check if data already loaded
    try:
        check_url = f"http://{ch_host}:{ch_port}/?user={ch_user}&password={urllib.parse.quote(ch_password)}&database={ch_db}"
        req = urllib.request.Request(check_url)
        req.data = b"SELECT count() FROM cloudtrail_events"
        with urllib.request.urlopen(req, timeout=10) as resp:
            existing = int(resp.read().strip())
            if existing > 0:
                print(f"CloudTrail data already loaded ({existing} records). Skipping.")
                sys.exit(0)
    except Exception:
        pass  # Table might not exist yet, that's fine

    total_records = 0
    batch = []
    insert_query = "INSERT INTO cloudtrail_events FORMAT JSONEachRow"

    for file_idx, filepath in enumerate(files, 1):
        print(f"Processing {filepath.name} ({file_idx}/{len(files)})...")
        try:
            with open(filepath, "r") as f:
                data = json.load(f)
        except json.JSONDecodeError as e:
            print(f"  WARNING: Skipping {filepath.name}: {e}")
            continue

        records = data.get("Records", [])
        file_count = 0

        for record in records:
            flat = flatten_record(record)
            batch.append(record_to_json_each_row(flat))
            file_count += 1

            if len(batch) >= batch_size:
                clickhouse_insert(ch_host, ch_port, ch_db, ch_user, ch_password, insert_query, "\n".join(batch))
                total_records += len(batch)
                print(f"  Inserted batch ({len(batch)} records, {total_records} total)")
                batch = []

        print(f"  Parsed {file_count} records from {filepath.name}")

    # Insert remaining batch
    if batch:
        clickhouse_insert(ch_host, ch_port, ch_db, ch_user, ch_password, insert_query, "\n".join(batch))
        total_records += len(batch)
        print(f"  Inserted final batch ({len(batch)} records, {total_records} total)")

    print(f"\nDone! Loaded {total_records} CloudTrail records into {ch_db}.cloudtrail_events")


if __name__ == "__main__":
    main()
