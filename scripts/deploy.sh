#!/bin/bash
# Server deploy script — called by GitHub Actions on the target server
set -e

ZIP_PATH="/tmp/release.zip"
EXTRACT_DIR="/tmp/release-$$"
RELAY_BIN="/opt/relay-platform/bin/relay-server"
RELAY_WEB="/opt/relay-platform/web/dist"
RELAY_USER="relay"

if [ ! -f "$ZIP_PATH" ]; then
    echo "Missing $ZIP_PATH"
    exit 1
fi

echo "Extracting $ZIP_PATH ..."
rm -rf "$EXTRACT_DIR"
unzip -o "$ZIP_PATH" -d "$EXTRACT_DIR" 2>&1 | tail -2

if [ ! -f "$EXTRACT_DIR/backend/relay-server" ]; then
    echo "relay-server not found in archive"
    exit 1
fi

echo "Stopping relay-platform ..."
sudo systemctl stop relay-platform.service

echo "Updating binary ..."
sudo cp "$EXTRACT_DIR/backend/relay-server" "$RELAY_BIN"
sudo chmod +x "$RELAY_BIN"

echo "Updating frontend ..."
sudo rm -rf "$RELAY_WEB"
sudo cp -r "$EXTRACT_DIR/frontend/dist" "$RELAY_WEB"

echo "Fixing permissions ..."
sudo chown -R "$RELAY_USER:$RELAY_USER" /opt/relay-platform

echo "Starting relay-platform ..."
sudo systemctl start relay-platform.service
sleep 2

HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:8080/)
if [ "$HTTP_CODE" != "200" ]; then
    echo "Deploy finished but HTTP $HTTP_CODE — check logs: sudo journalctl -u relay-platform -f"
    exit 1
fi
echo "Service OK (HTTP $HTTP_CODE)"

echo "Triggering source health check ..."
sleep 2
TOKEN=$(curl -s -X POST http://127.0.0.1:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@relay.io","password":"admin123456"}' 2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin)['accessToken'])")
curl -s -X POST -H "Authorization: Bearer $TOKEN" \
  http://127.0.0.1:8080/api/admin/sources/s_001/check > /dev/null
echo "Source health check done"
