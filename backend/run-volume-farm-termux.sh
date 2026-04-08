#!/usr/bin/env bash
# Run script for Termux - Volume Farming Bot with Non-Sleep Mode
# Usage: ./run-volume-farm-termux.sh [start|stop|restart|status|logs|daemon]

set -e

# Configuration
APP_NAME="volume-farm"
CMD_DIR="./cmd/volume-farm"
CONFIG_FILE="${2:-./config/volume-farm-config.yaml}"
LOG_FILE="volume-farm.log"
PID_FILE="volume-farm.pid"
BIN_PATH="./bin/${APP_NAME}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[OK]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

acquire_wake_lock() {
    if command -v termux-wake-lock &> /dev/null; then
        termux-wake-lock
        log_success "Wake lock acquired (screen off = keep running)"
    else
        log_warn "termux-wake-lock not found. Bot may stop when screen off."
        log_info "Install: pkg install termux-api"
    fi
}

release_wake_lock() {
    if command -v termux-wake-unlock &> /dev/null; then
        termux-wake-unlock
        log_info "Wake lock released"
    fi
}

build() {
    log_info "Building ${APP_NAME}..."
    go build -o "${BIN_PATH}" "${CMD_DIR}"
    log_success "Build complete: ${BIN_PATH}"
}

start() {
    if [ -f "${PID_FILE}" ] && kill -0 "$(cat "${PID_FILE}")" 2>/dev/null; then
        log_warn "Volume farming bot is already running (PID: $(cat "${PID_FILE}"))"
        exit 0
    fi

    # Always rebuild to ensure latest code changes are included
    build

    # Check config exists
    if [ ! -f "${CONFIG_FILE}" ]; then
        log_error "Config file not found: ${CONFIG_FILE}"
        log_info "Create one from: cp config/volume-farm-config.yaml.example ${CONFIG_FILE}"
        exit 1
    fi

    acquire_wake_lock

    log_info "Starting volume farming bot with config: ${CONFIG_FILE}"
    log_info "Features: Auto-discover symbols, Grid trading, Volume farming"
    
    # Start with nohup to survive terminal close
    nohup "${BIN_PATH}" -config "${CONFIG_FILE}" > "${LOG_FILE}" 2>&1 &
    PID=$!
    echo ${PID} > "${PID_FILE}"
    
    sleep 2
    
    if kill -0 ${PID} 2>/dev/null; then
        log_success "Volume farming bot started successfully (PID: ${PID})"
        log_info "View logs: ${0} logs"
        log_info "Stop bot: ${0} stop"
        log_info "Check status: ${0} status"
    else
        log_error "Bot failed to start. Check logs: ${LOG_FILE}"
        rm -f "${PID_FILE}"
        release_wake_lock
        exit 1
    fi
}

stop() {
    if [ ! -f "${PID_FILE}" ]; then
        # Try to find and kill by process name
        PID=$(pgrep -f "${APP_NAME}" || true)
        if [ -n "${PID}" ]; then
            log_info "Stopping volume farming bot processes..."
            kill ${PID} 2>/dev/null || true
            log_success "Bot stopped"
        else
            log_warn "No volume farming bot process found"
        fi
        release_wake_lock
        return 0
    fi

    PID=$(cat "${PID_FILE}")
    if kill -0 "${PID}" 2>/dev/null; then
        log_info "Stopping bot (PID: ${PID})..."
        kill "${PID}" 2>/dev/null || true
        sleep 2
        
        # Force kill if still running
        if kill -0 "${PID}" 2>/dev/null; then
            log_warn "Force killing bot..."
            kill -9 "${PID}" 2>/dev/null || true
        fi
        
        log_success "Volume farming bot stopped"
    else
        log_warn "Bot was not running"
    fi
    
    rm -f "${PID_FILE}"
    release_wake_lock
}

restart() {
    log_info "Restarting volume farming bot..."
    stop
    sleep 1
    start
}

status() {
    if [ -f "${PID_FILE}" ]; then
        PID=$(cat "${PID_FILE}")
        if kill -0 "${PID}" 2>/dev/null; then
            log_success "Volume farming bot is running (PID: ${PID})"
            log_info "Uptime: $(ps -o etime= -p ${PID} 2>/dev/null || echo 'unknown')"
            log_info "Log file: ${LOG_FILE}"
            log_info "Config: ${CONFIG_FILE}"
            
            # Show recent activity
            if [ -f "${LOG_FILE}" ]; then
                log_info "Recent activity:"
                tail -n 5 "${LOG_FILE}" | grep -E "(INFO|ERROR|WARN)" | tail -n 3 || true
            fi
        else
            log_warn "Bot is not running (stale PID file)"
            rm -f "${PID_FILE}"
        fi
    else
        PID=$(pgrep -f "${APP_NAME}" || true)
        if [ -n "${PID}" ]; then
            log_success "Bot is running (PID: ${PID}) - no PID file"
        else
            log_warn "Volume farming bot is not running"
        fi
    fi
}

view_logs() {
    if [ ! -f "${LOG_FILE}" ]; then
        log_warn "Log file not found: ${LOG_FILE}"
        return 1
    fi
    
    log_info "Showing logs (Ctrl+C to exit)..."
    tail -f "${LOG_FILE}"
}

dashboard() {
    if command -v python3 &> /dev/null; then
        log_info "Starting dashboard (Ctrl+C to exit)..."
        BOT_LOG_FILE="${LOG_FILE}" python3 ./scripts/dashboard.py
    elif command -v python &> /dev/null; then
        log_info "Starting dashboard (Ctrl+C to exit)..."
        BOT_LOG_FILE="${LOG_FILE}" python ./scripts/dashboard.py
    else
        log_error "Python not found. Cannot run dashboard."
        log_info "Install Python: pkg install python"
        exit 1
    fi
}

daemon_mode() {
    log_info "Starting in daemon mode (auto-restart on crash)..."
    log_info "Press Ctrl+C to stop daemon watcher (bot will keep running)"
    
    acquire_wake_lock
    
    while true; do
        if ! pgrep -f "${APP_NAME}" > /dev/null; then
            log_warn "Bot not running. Restarting..."
            start
        fi
        sleep 30
    done
}

# Show usage
usage() {
    echo "Usage: $0 [command] [config_file]"
    echo ""
    echo "Commands:"
    echo "  start    - Start the volume farming bot with wake lock"
    echo "  stop     - Stop the bot and release wake lock"
    echo "  restart  - Restart the bot"
    echo "  status   - Check bot status"
    echo "  logs     - View live logs"
    echo "  dashboard - Real-time metrics dashboard (refresh 1s)"
    echo "  daemon   - Run in daemon mode (auto-restart)"
    echo "  build    - Build the binary"
    echo ""
    echo "Examples:"
    echo "  $0 start                                    # Start with default config"
    echo "  $0 start config/volume-farm-config.yaml     # Start with custom config"
    echo "  $0 logs                                     # View logs"
    echo "  $0 dashboard                                # Real-time dashboard"
    echo "  $0 stop                                     # Stop the bot"
    echo ""
    echo "Note: This script uses termux-wake-lock to keep running when screen is off"
    echo "      The bot will auto-discover symbols and start grid trading."
}

# Main
COMMAND=${1:-start}

# If second argument is a file, use it as config
if [ -n "$2" ] && [ -f "$2" ]; then
    CONFIG_FILE="$2"
fi

case "${COMMAND}" in
    start)
        start
        ;;
    stop)
        stop
        ;;
    restart)
        restart
        ;;
    status)
        status
        ;;
    logs)
        view_logs
        ;;
    dashboard)
        dashboard
        ;;
    daemon)
        daemon_mode
        ;;
    build)
        build
        ;;
    help|--help|-h)
        usage
        ;;
    *)
        log_error "Unknown command: ${COMMAND}"
        usage
        exit 1
        ;;
esac
