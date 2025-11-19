#!/bin/bash

# Admin Cache Management Script
# Quick access to cache admin endpoints

BASE_URL="http://localhost"
API_URL="${BASE_URL}/api/v1"

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo "🔐 Task Manager - Admin Cache Management"
echo "========================================"
echo ""

ADMIN_EMAIL="test_admin_001@example.com"
ADMIN_PASSWORD="AdminPass@123"

echo "1️⃣  Logging in as admin..."
LOGIN_RESPONSE=$(curl -s -X POST "${API_URL}/auth/login" \
  -H "Content-Type: application/json" \
  -d "{
    \"email\": \"${ADMIN_EMAIL}\",
    \"password\": \"${ADMIN_PASSWORD}\"
  }")

TOKEN=$(echo "$LOGIN_RESPONSE" | jq -r '.access_token')

if [ "$TOKEN" != "null" ] && [ -n "$TOKEN" ]; then
  echo -e "${GREEN}✅ Admin login successful${NC}"
  echo "Token: ${TOKEN:0:30}..."
  echo ""
  
  PERMISSIONS=$(echo "$LOGIN_RESPONSE" | jq -r '.permissions[]' | grep -E "system:admin|audit:read|role:manage" | wc -l)
  if [ "$PERMISSIONS" -ge 3 ]; then
    echo -e "${GREEN}✅ Admin permissions verified${NC}\n"
  else
    echo -e "${RED}⚠️  Warning: User may not have full admin permissions${NC}\n"
  fi
else
  echo -e "${RED}❌ Admin login failed${NC}"
  echo "Response: $LOGIN_RESPONSE" | jq .
  echo ""
  echo "💡 To create admin user, run:"
  echo "   curl -X POST http://localhost:80/api/v1/auth/register -H \"Content-Type: application/json\" \\"
  echo "     -d '{\"username\":\"test_admin_001\",\"email\":\"test_admin_001@example.com\",\"password\":\"AdminPass@123\",\"first_name\":\"Test\",\"last_name\":\"Admin\"}'"
  echo ""
  echo "   Then promote to admin:"
  echo "   docker exec -it task-manager-postgres psql -U postgres -d task_manager \\"
  echo "     -c \"INSERT INTO user_roles (user_id, role_id) SELECT u.id, '00000000-0000-0000-0000-000000000002' FROM users u WHERE u.email = 'test_admin_001@example.com';\""
  exit 1
fi

echo "Choose an option:"
echo "  1) Get cache stats"
echo "  2) Get cache health"
echo "  3) Trigger cache warming"
echo "  4) Clear cache"
echo "  5) Enqueue warmup job"
echo "  6) Enqueue batch warmup"
echo "  7) Schedule warmup job"
echo "  8) Evict cache key"
echo "  0) Run all tests"
echo ""
read -p "Enter choice [0-8]: " choice
echo ""

case $choice in
  1)
    echo "📊 Cache Statistics"
    echo -e "${YELLOW}curl -X GET ${API_URL}/cache/stats${NC}"
    curl -X GET "${API_URL}/cache/stats" \
      -H "Authorization: Bearer $TOKEN" | jq .
    ;;
    
  2)
    echo "💚 Cache Health"
    echo -e "${YELLOW}curl -X GET ${API_URL}/cache/health${NC}"
    curl -X GET "${API_URL}/cache/health" \
      -H "Authorization: Bearer $TOKEN" | jq .
    ;;
    
  3)
    echo "🔥 Trigger Cache Warming"
    echo -e "${YELLOW}curl -X POST ${API_URL}/cache/warm${NC}"
    curl -X POST "${API_URL}/cache/warm" \
      -H "Authorization: Bearer $TOKEN" | jq .
    ;;
    
  4)
    echo "🧹 Clear Cache"
    read -p "Are you sure you want to clear all cache? (yes/no): " confirm
    if [ "$confirm" = "yes" ]; then
      echo -e "${YELLOW}curl -X DELETE ${API_URL}/cache/clear${NC}"
      curl -X DELETE "${API_URL}/cache/clear" \
        -H "Authorization: Bearer $TOKEN" | jq .
    else
      echo "Cancelled."
    fi
    ;;
    
  5)
    echo "➕ Enqueue Warmup Job"
    echo -e "${YELLOW}curl -X POST ${API_URL}/cache/jobs/warmup${NC}"
    curl -X POST "${API_URL}/cache/jobs/warmup" \
      -H "Authorization: Bearer $TOKEN" \
      -H "Content-Type: application/json" \
      -d '{
        "key": "test:key",
        "data": {"test": "data"},
        "ttl": 900000000000,
        "priority": 5
      }' | jq .
    ;;
    
  6)
    echo "📦 Enqueue Batch Warmup"
    echo -e "${YELLOW}curl -X POST ${API_URL}/cache/jobs/batch${NC}"
    curl -X POST "${API_URL}/cache/jobs/batch" \
      -H "Authorization: Bearer $TOKEN" \
      -H "Content-Type: application/json" \
      -d '{
        "keys": ["batch:1", "batch:2", "batch:3"],
        "data": {},
        "priority": 5
      }' | jq .
    ;;
    
  7)
    echo "⏰ Schedule Warmup Job"
    FUTURE_TIME=$(date -u -v+10M +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || date -u -d "+10 minutes" +"%Y-%m-%dT%H:%M:%SZ")
    echo "Scheduling for: $FUTURE_TIME"
    echo -e "${YELLOW}curl -X POST ${API_URL}/cache/jobs/scheduled${NC}"
    curl -X POST "${API_URL}/cache/jobs/scheduled" \
      -H "Authorization: Bearer $TOKEN" \
      -H "Content-Type: application/json" \
      -d "{
        \"key\": \"scheduled:test\",
        \"data\": {},
        \"ttl\": 900000000000,
        \"process_at\": \"${FUTURE_TIME}\",
        \"priority\": 3
      }" | jq .
    ;;
    
  8)
    echo "️  Evict Cache Key"
    read -p "Enter cache key (or pattern with *): " cache_key
    if [ -n "$cache_key" ]; then
      echo -e "${YELLOW}curl -X DELETE ${API_URL}/cache/jobs/evict/${cache_key}${NC}"
      curl -X DELETE "${API_URL}/cache/jobs/evict/${cache_key}" \
        -H "Authorization: Bearer $TOKEN" | jq .
    else
      echo "No key provided."
    fi
    ;;
    
  0)
    echo "🧪 Running All Tests..."
    echo ""
    
    echo "1️⃣  Cache Stats"
    curl -s -X GET "${API_URL}/cache/stats" -H "Authorization: Bearer $TOKEN" | jq .
    echo ""
    
    echo "2️⃣  Cache Health"
    curl -s -X GET "${API_URL}/cache/health" -H "Authorization: Bearer $TOKEN" | jq .
    echo ""
    
    echo "3️⃣  Trigger Warming"
    curl -s -X POST "${API_URL}/cache/warm" -H "Authorization: Bearer $TOKEN" | jq .
    echo ""
    
    echo "4️⃣  Enqueue Warmup Job"
    curl -s -X POST "${API_URL}/cache/jobs/warmup" \
      -H "Authorization: Bearer $TOKEN" \
      -H "Content-Type: application/json" \
      -d '{"key":"test:all","data":{},"ttl":900000000000,"priority":5}' | jq .
    echo ""
    
    echo "5️⃣  Enqueue Batch Job"
    curl -s -X POST "${API_URL}/cache/jobs/batch" \
      -H "Authorization: Bearer $TOKEN" \
      -H "Content-Type: application/json" \
      -d '{"keys":["batch:test:1","batch:test:2"],"data":{},"priority":5}' | jq .
    echo ""
    
    echo "6️⃣  Schedule Future Job"
    FUTURE_TIME=$(date -u -v+5M +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || date -u -d "+5 minutes" +"%Y-%m-%dT%H:%M:%SZ")
    curl -s -X POST "${API_URL}/cache/jobs/scheduled" \
      -H "Authorization: Bearer $TOKEN" \
      -H "Content-Type: application/json" \
      -d "{\"key\":\"scheduled:test:all\",\"data\":{},\"ttl\":900000000000,\"process_at\":\"${FUTURE_TIME}\",\"priority\":3}" | jq .
    echo ""
    
    echo -e "${GREEN}✅ All tests completed${NC}"
    ;;
    
  *)
    echo "Invalid choice"
    exit 1
    ;;
esac

echo ""
echo "======================================"
echo -e "${GREEN}✅ Done!${NC}"
echo ""
echo "💡 Tips:"
echo "  - Token expires in 1 hour"
echo "  - Admin permissions required for cache endpoints"
echo "  - Use 'jq' to format JSON output"
