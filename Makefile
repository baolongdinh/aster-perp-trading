.PHONY: dashboard dashboard-terminal dashboard-gui dashboard-ws help

# Launch real-time dashboard (HTML + WebSocket)
dashboard:
	@echo "============================================"
	@echo "Starting Grid Trading Dashboard (HTML)"
	@echo "============================================"
	@echo ""
	@echo "Features:"
	@echo "  • Modern HTML/CSS/JS interface"
	@echo "  • Real-time metrics via WebSocket"
	@echo "  • Beautiful gradient design"
	@echo "  • Symbol states & market conditions"
	@echo "  • State transitions history"
	@echo "  • Auto-reconnect on disconnect"
	@echo ""
	@echo "Requirements:"
	@echo "  • Bot must be running (WebSocket server on :8083)"
	@echo ""
	@echo "Opening dashboard in browser..."
	@if command -v xdg-open >/dev/null 2>&1; then \
		xdg-open dashboard.html; \
	elif command -v open >/dev/null 2>&1; then \
		open dashboard.html; \
	elif command -v start >/dev/null 2>&1; then \
		start dashboard.html; \
	else \
		echo "Please open dashboard.html in your browser manually"; \
		echo "File location: $$(pwd)/dashboard.html"; \
	fi

# Terminal Dashboard (Python - requires websockets)
dashboard-terminal:
	@echo "============================================"
	@echo "Starting Grid Trading Dashboard (Terminal)"
	@echo "============================================"
	@echo ""
	@echo "Features:"
	@echo "  • Real-time metrics via WebSocket"
	@echo "  • Terminal-based (no GUI needed)"
	@echo "  • Symbol states & market conditions"
	@echo "  • State transitions history"
	@echo "  • Auto-reconnect on disconnect"
	@echo ""
	@echo "Requirements:"
	@echo "  • Bot must be running (WebSocket server on :8083)"
	@echo "  • Python websockets: pip install websockets"
	@echo ""
	@if command -v python3 >/dev/null 2>&1; then \
		python3 dashboard_ws.py; \
	elif command -v python >/dev/null 2>&1; then \
		python dashboard_ws.py; \
	else \
		echo "ERROR: Python not found. Install with: sudo apt install python3"; \
		exit 1; \
	fi

# GUI Dashboard (Tkinter - requires tkinter)
dashboard-gui:
	@echo "============================================"
	@echo "Starting Grid Trading Dashboard (GUI)"
	@echo "============================================"
	@echo ""
	@echo "Note: Requires tkinter: sudo apt install python3-tk"
	@echo ""
	@if command -v python3 >/dev/null 2>&1; then \
		python3 dashboard_gui.py; \
	elif command -v python >/dev/null 2>&1; then \
		python dashboard_gui.py; \
	else \
		echo "ERROR: Python not found. Install with: sudo apt install python3"; \
		exit 1; \
	fi

# Legacy curses dashboard (log file based)
dashboard-ws:
	@echo "============================================"
	@echo "Starting Legacy Dashboard (Curses)"
	@echo "============================================"
	@echo ""
	@echo "Note: This version reads from log files"
	@echo "For real-time WebSocket, use 'make dashboard'"
	@echo ""
	@if command -v python3 >/dev/null 2>&1; then \
		python3 dashboard.py; \
	elif command -v python >/dev/null 2>&1; then \
		python dashboard.py; \
	else \
		echo "ERROR: Python not found. Install with: sudo apt install python3"; \
		exit 1; \
	fi

# Show help
help:
	@echo "╔════════════════════════════════════════════════════════════════╗"
	@echo "║           Aster Perp Trading - Make Commands                    ║"
	@echo "╚════════════════════════════════════════════════════════════════╝"
	@echo ""
	@echo "📊 DASHBOARD:"
	@echo "  make dashboard            - Launch real-time trading dashboard"
	@echo ""
	@echo "🔧 BACKEND (cd backend first):"
	@echo "  make build               - Build bot binary"
	@echo "  make run                 - Run bot directly"
	@echo "  make agentic-run         - Run agentic + volume farm bot"
	@echo "  make test                - Run all tests"
	@echo "  make test-unit           - Run unit tests only"
	@echo "  make clean               - Clean build artifacts"
	@echo ""
	@echo "📝 For more commands, see backend/Makefile"
