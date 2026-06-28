#!/bin/sh
# dev/opensearch-seed.sh
# Seeds OpenSearch with sample data for development and testing.

set -e

OS_HOST="http://aether-opensearch:9200"

echo "Waiting for OpenSearch..."
until curl -sf "$OS_HOST/_cluster/health?wait_for_status=yellow&timeout=5s" > /dev/null 2>&1; do
  sleep 2
done
echo "OpenSearch is ready."

# Create ecommerce index
echo "Creating ecommerce index..."
curl -sf -X PUT "$OS_HOST/ecommerce" -H 'Content-Type: application/json' -d '{
  "mappings": {
    "properties": {
      "product_name": {"type": "text"},
      "category": {"type": "keyword"},
      "price": {"type": "float"},
      "quantity": {"type": "integer"},
      "order_date": {"type": "date"},
      "customer_name": {"type": "text"},
      "status": {"type": "keyword"}
    }
  }
}'

# Bulk index ecommerce data
echo "Indexing ecommerce documents..."
curl -sf -X POST "$OS_HOST/_bulk" -H 'Content-Type: application/x-ndjson' -d '
{"index":{"_index":"ecommerce"}}
{"product_name":"Laptop Pro 15","category":"Electronics","price":1299.99,"quantity":1,"order_date":"2024-01-15","customer_name":"Alice Johnson","status":"shipped"}
{"index":{"_index":"ecommerce"}}
{"product_name":"Wireless Mouse","category":"Electronics","price":29.99,"quantity":3,"order_date":"2024-01-16","customer_name":"Bob Smith","status":"delivered"}
{"index":{"_index":"ecommerce"}}
{"product_name":"Standing Desk","category":"Furniture","price":549.00,"quantity":1,"order_date":"2024-01-17","customer_name":"Carol White","status":"processing"}
{"index":{"_index":"ecommerce"}}
{"product_name":"Mechanical Keyboard","category":"Electronics","price":149.99,"quantity":2,"order_date":"2024-01-18","customer_name":"David Brown","status":"shipped"}
{"index":{"_index":"ecommerce"}}
{"product_name":"Monitor 27 inch","category":"Electronics","price":399.99,"quantity":1,"order_date":"2024-01-19","customer_name":"Eve Davis","status":"delivered"}
{"index":{"_index":"ecommerce"}}
{"product_name":"Desk Chair","category":"Furniture","price":299.99,"quantity":1,"order_date":"2024-01-20","customer_name":"Frank Miller","status":"shipped"}
{"index":{"_index":"ecommerce"}}
{"product_name":"USB-C Hub","category":"Electronics","price":49.99,"quantity":5,"order_date":"2024-01-21","customer_name":"Grace Wilson","status":"delivered"}
{"index":{"_index":"ecommerce"}}
{"product_name":"Webcam HD","category":"Electronics","price":79.99,"quantity":2,"order_date":"2024-01-22","customer_name":"Henry Taylor","status":"processing"}
{"index":{"_index":"ecommerce"}}
{"product_name":"Bookshelf","category":"Furniture","price":189.99,"quantity":1,"order_date":"2024-01-23","customer_name":"Ivy Anderson","status":"shipped"}
{"index":{"_index":"ecommerce"}}
{"product_name":"Noise Cancelling Headphones","category":"Electronics","price":249.99,"quantity":1,"order_date":"2024-01-24","customer_name":"Jack Thomas","status":"delivered"}
'

# Create logs index
echo "Creating logs index..."
curl -sf -X PUT "$OS_HOST/logs" -H 'Content-Type: application/json' -d '{
  "mappings": {
    "properties": {
      "timestamp": {"type": "date"},
      "level": {"type": "keyword"},
      "message": {"type": "text"},
      "service": {"type": "keyword"},
      "response_time_ms": {"type": "integer"}
    }
  }
}'

# Bulk index log data
echo "Indexing log documents..."
curl -sf -X POST "$OS_HOST/_bulk" -H 'Content-Type: application/x-ndjson' -d '
{"index":{"_index":"logs"}}
{"timestamp":"2024-01-15T10:00:00Z","level":"INFO","message":"Request processed successfully","service":"api-gateway","response_time_ms":45}
{"index":{"_index":"logs"}}
{"timestamp":"2024-01-15T10:01:00Z","level":"ERROR","message":"Database connection timeout","service":"user-service","response_time_ms":5000}
{"index":{"_index":"logs"}}
{"timestamp":"2024-01-15T10:02:00Z","level":"WARN","message":"High memory usage detected","service":"payment-service","response_time_ms":120}
{"index":{"_index":"logs"}}
{"timestamp":"2024-01-15T10:03:00Z","level":"INFO","message":"User login successful","service":"auth-service","response_time_ms":89}
{"index":{"_index":"logs"}}
{"timestamp":"2024-01-15T10:04:00Z","level":"INFO","message":"Order created","service":"order-service","response_time_ms":200}
{"index":{"_index":"logs"}}
{"timestamp":"2024-01-15T10:05:00Z","level":"ERROR","message":"Failed to send notification","service":"notification-service","response_time_ms":3000}
{"index":{"_index":"logs"}}
{"timestamp":"2024-01-15T10:06:00Z","level":"INFO","message":"Cache refreshed","service":"api-gateway","response_time_ms":15}
{"index":{"_index":"logs"}}
{"timestamp":"2024-01-15T10:07:00Z","level":"DEBUG","message":"Query executed","service":"search-service","response_time_ms":30}
{"index":{"_index":"logs"}}
{"timestamp":"2024-01-15T10:08:00Z","level":"WARN","message":"Rate limit approaching","service":"api-gateway","response_time_ms":50}
{"index":{"_index":"logs"}}
{"timestamp":"2024-01-15T10:09:00Z","level":"INFO","message":"Batch job completed","service":"scheduler","response_time_ms":15000}
'

echo "Seed complete!"
echo "  ecommerce index: 10 documents"
echo "  logs index: 10 documents"
echo ""
echo "Try: SELECT * FROM ecommerce WHERE price > 100"
echo "Try: SELECT level, count(*) FROM logs GROUP BY level"
