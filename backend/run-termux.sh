#!/data/data/com.termux/files/usr/bin/bash
# Run script for Termux - Backend Bot with Non-Sleep Mode
# Usage: ./run-termux.sh [start|stop|restart|status|logs]

set -e

# Configuration
APP_NAME="aster-bot"
CMD_DIR="./cmd/bot"
CONFIG_FILE="${2:-config.yaml}"
LOG_FILE="bot.log"
PID_FILE="bot.pid"
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
        log_warn "Bot is already running (PID: $(cat "${PID_FILE}"))"
        exit 0
    fi

    # Build if binary doesn't exist
    if [ ! -f "${BIN_PATH}" ]; then
        build
    fi

    # Check config exists
    if [ ! -f "${CONFIG_FILE}" ]; then
        log_error "Config file not found: ${CONFIG_FILE}"
        log_info "Create one from: cp config.example.yaml ${CONFIG_FILE}"
        exit 1
    fi

    acquire_wake_lock

    log_info "Starting bot with config: ${CONFIG_FILE}"
    
    # Start with nohup to survive terminal close
    nohup "${BIN_PATH}" "${CONFIG_FILE}" > "${LOG_FILE}" 2>&1 &
    PID=$!
    echo ${PID} > "${PID_FILE}"
    
    sleep 1
    
    if kill -0 ${PID} 2>/dev/null; then
        log_success "Bot started successfully (PID: ${PID})"
        log_info "View logs: ${0} logs"
        log_info "Stop bot: ${0} stop"
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
            log_info "Stopping bot processes..."
            kill ${PID} 2>/dev/null || true
            log_success "Bot stopped"
        else
            log_warn "No bot process found"
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
        
        log_success "Bot stopped"
    else
        log_warn "Bot was not running"
    fi
    
    rm -f "${PID_FILE}"
    release_wake_lock
}

restart() {
    log_info "Restarting bot..."
    stop
    sleep 1
    start
}

status() {
    if [ -f "${PID_FILE}" ]; then
        PID=$(cat "${PID_FILE}")
        if kill -0 "${PID}" 2>/dev/null; then
            log_success "Bot is running (PID: ${PID})"
            log_info "Uptime: $(ps -o etime= -p ${PID} 2>/dev/null || echo 'unknown')"
            log_info "Log file: ${LOG_FILE}"
            log_info "Config: ${CONFIG_FILE}"
        else
            log_warn "Bot is not running (stale PID file)"
            rm -f "${PID_FILE}"
        fi
    else
        PID=$(pgrep -f "${APP_NAME}" || true)
        if [ -n "${PID}" ]; then
            log_success "Bot is running (PID: ${PID}) - no PID file"
        else
            log_warn "Bot is not running"
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

daemon_mode() {
    log_info "Starting in daemon mode (auto-restart on crash)..."
    
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
    echo "  start    - Start the bot with wake lock (non-sleep mode)"
    echo "  stop     - Stop the bot and release wake lock"
    echo "  restart  - Restart the bot"
    echo "  status   - Check bot status"
    echo "  logs     - View live logs"
    echo "  daemon   - Run in daemon mode (auto-restart)"
    echo "  build    - Build the binary"
    echo ""
    echo "Examples:"
    echo "  $0 start                    # Start with default config.yaml"
    echo "  $0 start my-config.yaml     # Start with custom config"
    echo "  $0 logs                     # View logs"
    echo "  $0 stop                     # Stop the bot"
    echo ""
    echo "Note: This script uses termux-wake-lock to keep running when screen is off"
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
