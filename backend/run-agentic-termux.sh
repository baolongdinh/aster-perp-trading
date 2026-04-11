#!/usr/bin/env bash
# Run script for Termux - Agentic + Volume Farm Trading Bot
# Usage: ./run-agentic-termux.sh [start|start-test|start-vf|stop|restart|status|logs|daemon]

set -e

# Configuration
APP_NAME="agentic"
CMD_DIR="./cmd/agentic"
CONFIG_FILE="${2:-./config/agentic-vf-config.yaml}"
LOG_FILE="agentic-vf.log"
PID_FILE="agentic-vf.pid"
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
        log_warn "Agentic + VF bot is already running (PID: $(cat "${PID_FILE}"))"
        exit 0
    fi

    # Always rebuild to ensure latest code changes are included
    build

    # Check config exists
    if [ ! -f "${CONFIG_FILE}" ]; then
        log_error "Config file not found: ${CONFIG_FILE}"
        log_info "Create one from: cp config/agentic-vf-config.yaml config/agentic-vf-config.yaml"
        exit 1
    fi

    acquire_wake_lock

    log_info "Starting Agentic + Volume Farm bot with config: ${CONFIG_FILE}"
    log_info "Features: Regime detection, Dynamic whitelist, VF execution"
    
    # Start with nohup to survive terminal close
    nohup "${BIN_PATH}" -config "${CONFIG_FILE}" > "${LOG_FILE}" 2>&1 &
    PID=$!
    echo ${PID} > "${PID_FILE}"
    
    sleep 2
    
    if kill -0 ${PID} 2>/dev/null; then
        log_success "Agentic + VF bot started successfully (PID: ${PID})"
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

start_test() {
    if [ -f "${PID_FILE}" ] && kill -0 "$(cat "${PID_FILE}")" 2>/dev/null; then
        log_warn "Agentic + VF bot is already running (PID: $(cat "${PID_FILE}"))"
        exit 0
    fi

    build

    if [ ! -f "${CONFIG_FILE}" ]; then
        log_error "Config file not found: ${CONFIG_FILE}"
        exit 1
    fi

    acquire_wake_lock

    log_info "Starting Agentic + VF bot in TEST MODE (no real trades)"
    
    nohup "${BIN_PATH}" -config "${CONFIG_FILE}" -dry-run > "${LOG_FILE}" 2>&1 &
    PID=$!
    echo ${PID} > "${PID_FILE}"
    
    sleep 2
    
    if kill -0 ${PID} 2>/dev/null; then
        log_success "Agentic + VF bot (TEST MODE) started (PID: ${PID})"
        log_info "View logs: ${0} logs"
        log_info "Stop bot: ${0} stop"
    else
        log_error "Bot failed to start. Check logs: ${LOG_FILE}"
        rm -f "${PID_FILE}"
        release_wake_lock
        exit 1
    fi
}

start_vf() {
    if [ -f "${PID_FILE}" ] && kill -0 "$(cat "${PID_FILE}")" 2>/dev/null; then
        log_warn "Agentic + VF bot is already running (PID: $(cat "${PID_FILE}"))"
        exit 0
    fi

    build

    if [ ! -f "${CONFIG_FILE}" ]; then
        log_error "Config file not found: ${CONFIG_FILE}"
        exit 1
    fi

    acquire_wake_lock

    log_info "Starting Volume Farm ONLY mode (no Agentic layer)"
    
    nohup "${BIN_PATH}" -config "${CONFIG_FILE}" -vf-only > "${LOG_FILE}" 2>&1 &
    PID=$!
    echo ${PID} > "${PID_FILE}"
    
    sleep 2
    
    if kill -0 ${PID} 2>/dev/null; then
        log_success "Volume Farm bot started (PID: ${PID})"
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
            log_info "Stopping agentic bot processes..."
            kill ${PID} 2>/dev/null || true
            log_success "Bot stopped"
        else
            log_warn "No agentic bot process found"
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
        
        log_success "Agentic bot stopped"
    else
        log_warn "Bot was not running"
    fi
    
    rm -f "${PID_FILE}"
    release_wake_lock
}

restart() {
    log_info "Restarting agentic bot..."
    stop
    sleep 1
    start
}

status() {
    if [ -f "${PID_FILE}" ]; then
        PID=$(cat "${PID_FILE}")
        if kill -0 "${PID}" 2>/dev/null; then
            log_success "Agentic bot is running (PID: ${PID})"
            log_info "Uptime: $(ps -o etime= -p ${PID} 2>/dev/null || echo 'unknown')"
            log_info "Log file: ${LOG_FILE}"
            log_info "Config: ${CONFIG_FILE}"
            log_info "Symbol: ${SYMBOL}"
            
            # Show recent activity
            if [ -f "${LOG_FILE}" ]; then
                log_info "Recent activity:"
                tail -n 5 "${LOG_FILE}" | grep -E "(INFO|ERROR|WARN|Regime|Decision)" | tail -n 3 || true
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
            log_warn "Agentic bot is not running"
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
    echo "  start      - Start Agentic + Volume Farm with wake lock"
    echo "  start-test - Start in test mode (no real trades)"
    echo "  start-vf   - Start Volume Farm only (no Agentic layer)"
    echo "  stop       - Stop the bot and release wake lock"
    echo "  restart    - Restart the bot"
    echo "  status     - Check bot status"
    echo "  logs       - View live logs"
    echo "  daemon     - Run in daemon mode (auto-restart)"
    echo "  build      - Build the binary"
    echo ""
    echo "Examples:"
    echo "  $0 start                                    # Start with default config"
    echo "  $0 start config/agentic-vf-config.yaml      # Start with custom config"
    echo "  $0 start-test                               # Test mode (no real trades)"
    echo "  $0 start-vf                                 # VF only mode"
    echo "  $0 logs                                     # View logs"
    echo "  $0 stop                                     # Stop the bot"
    echo ""
    echo "Note: This script uses termux-wake-lock to keep running when screen is off"
    echo "      The bot uses Agentic layer for regime detection and dynamic whitelist."
}

# Main
COMMAND=${1:-start}

# If second argument is a file, use it as config
if [ -n "$2" ] && [ -f "$2" ]; then
    CONFIG_FILE="$2"
fi

# If third argument provided, use as symbol
if [ -n "$3" ]; then
    SYMBOL="$3"
fi

case "${COMMAND}" in
    start)
        start
        ;;
    start-test)
        start_test
        ;;
    start-vf)
        start_vf
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
