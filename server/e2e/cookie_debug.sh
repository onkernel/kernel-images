#!/bin/bash

echo "=== Container User Info ==="
echo "Current user: $(whoami)"
echo "User ID: $(id)"
echo "Home directory: $HOME"
echo ""

echo "=== Chromium Process Info ==="
echo "Chromium processes:"
ps aux | grep -i chromium | grep -v grep || echo "No chromium processes found"
echo ""

echo "=== User Data Directory Info ==="
USER_DATA_DIR="/home/kernel/user-data"
if [ -d "$USER_DATA_DIR" ]; then
    echo "User data directory exists: $USER_DATA_DIR"
    echo "Owner: $(ls -ld "$USER_DATA_DIR")"
    echo "Contents:"
    ls -la "$USER_DATA_DIR" || echo "Failed to list contents"
    
    COOKIES_DIR="$USER_DATA_DIR/Default"
    if [ -d "$COOKIES_DIR" ]; then
        echo ""
        echo "Default directory exists: $COOKIES_DIR"
        echo "Owner: $(ls -ld "$COOKIES_DIR")"
        echo "Contents:"
        ls -la "$COOKIES_DIR" || echo "Failed to list contents"
        
        COOKIES_FILE="$COOKIES_DIR/Cookies"
        if [ -f "$COOKIES_FILE" ]; then
			echo ""
			echo "Cookies file exists: $COOKIES_FILE"
			echo "Owner: $(ls -ld "$COOKIES_FILE")"
			echo "Modification time: $(stat -c %y "$COOKIES_FILE")"
			echo "File size: $(stat -c %s "$COOKIES_FILE") bytes"
			if command -v file >/dev/null 2>&1; then
				echo "File type: $(file "$COOKIES_FILE")"
			else
				echo "File type: unknown (file command not found)"
			fi
			
			# Try to inspect the cookies database for specific cookies
			echo ""
			echo "=== Cookie Database Inspection ==="
			if command -v sqlite3 >/dev/null 2>&1; then
				echo "Checking for e2e_cookie:"
				sqlite3 "$COOKIES_FILE" "SELECT name, value, host_key, path, expires_utc FROM cookies WHERE name='e2e_cookie';" 2>/dev/null || echo "Failed to query e2e_cookie"
				echo "Checking for .x.com cookie:"
				sqlite3 "$COOKIES_FILE" "SELECT name, value, host_key, path, expires_utc FROM cookies WHERE host_key='.x.com';" 2>/dev/null || echo "Failed to query .x.com cookie"
				echo "All cookies in database:"
				sqlite3 "$COOKIES_FILE" "SELECT name, value, host_key, path FROM cookies LIMIT 10;" 2>/dev/null || echo "Failed to query cookies"
			else
				echo "sqlite3 not available, cannot inspect cookie database"
			fi
		else
			echo ""
			echo "Cookies file does not exist: $COOKIES_FILE"
		fi
    else
        echo ""
        echo "Default directory does not exist: $COOKIES_DIR"
    fi
else
    echo "User data directory does not exist: $USER_DATA_DIR"
fi

echo ""
echo "=== Supervisor Status ==="
supervisorctl -c /etc/supervisor/supervisord.conf status chromium || true

echo ""
echo "=== Environment Variables ==="
echo "RUN_AS_ROOT: ${RUN_AS_ROOT:-not set}"
echo "USER: ${USER:-not set}"
echo "HOME: ${HOME:-not set}"
echo "PWD: $PWD"
