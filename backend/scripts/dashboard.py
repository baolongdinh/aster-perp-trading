#!/usr/bin/env python3
"""Grid Trading Dashboard - Logfmt Parser"""

import curses
import os
import re
import signal
import sys
from datetime import datetime
from collections import defaultdict

class DashboardApp:
    def __init__(self, stdscr, log_file=None):
        self.stdscr = stdscr
        self.running = True

        # Auto-detect log file if not provided
        if log_file is None:
            log_file = self.find_log_file()
            if log_file is None:
                raise FileNotFoundError("Cannot find log file. Please specify log file path or ensure bot is running.")

        self.log_file = log_file
        self.metrics = {
            'total_volume': 0.0, 'orders_placed': 0, 'orders_filled': 0,
            'fill_rate': 0.0, 'active_orders': 0, 'positions': {},
            'symbols': set(), 'last_update': '', 'errors': 0, 'warnings': 0,
            'status': 'WAITING'
        }
        # Track per-symbol state and market conditions
        self.symbol_states = {}  # {symbol: {'state': 'TRADING', 'volatility_score': 0.5, ...}}
        self.state_transitions = []  # List of recent state transitions

    def find_log_file(self):
        """Auto-detect log file from common locations"""
        possible_paths = [
            'agentic-vf.log',
            'volume-farm.log',
            'logs/agentic-vf.log',
            'logs/volume-farm.log',
            'backend/agentic-vf.log',
            'backend/volume-farm.log',
        ]

        for path in possible_paths:
            if os.path.exists(path):
                return path

        return None

        curses.start_color()
        for i, (fg, bg) in enumerate([
            (curses.COLOR_GREEN, curses.COLOR_BLACK),
            (curses.COLOR_RED, curses.COLOR_BLACK),
            (curses.COLOR_YELLOW, curses.COLOR_BLACK),
            (curses.COLOR_CYAN, curses.COLOR_BLACK),
            (curses.COLOR_WHITE, curses.COLOR_BLACK),
            (curses.COLOR_MAGENTA, curses.COLOR_BLACK),
            (curses.COLOR_BLUE, curses.COLOR_BLACK),
            (curses.COLOR_BLACK, curses.COLOR_GREEN),
            (curses.COLOR_BLACK, curses.COLOR_RED),
            (curses.COLOR_BLACK, curses.COLOR_YELLOW),
        ], 1):
            curses.init_pair(i, fg, bg)
        
        self.GREEN = curses.color_pair(1)
        self.RED = curses.color_pair(2)
        self.YELLOW = curses.color_pair(3)
        self.CYAN = curses.color_pair(4)
        self.WHITE = curses.color_pair(5)
        self.MAGENTA = curses.color_pair(6)
        self.BLUE = curses.color_pair(7)
        self.BG_GREEN = curses.color_pair(8)
        self.BG_RED = curses.color_pair(9)
        self.BG_YELLOW = curses.color_pair(10)
        self.BOLD = curses.A_BOLD
        curses.curs_set(0)
        signal.signal(signal.SIGINT, lambda s, f: setattr(self, 'running', False))
        signal.signal(signal.SIGTERM, lambda s, f: setattr(self, 'running', False))
        
    def parse_logfmt(self, line):
        """Parse logfmt format: key=value key2="value2"""
        data = {}
        # Match key=value or key="value with spaces"
        pattern = r'(\w+)=([^\s"]+|"[^"]*")'
        for match in re.finditer(pattern, line):
            key = match.group(1)
            value = match.group(2).strip('"')
            data[key] = value
        return data
        
    def parse_log_file(self):
        if not os.path.exists(self.log_file):
            self.metrics['status'] = 'NO LOG'
            return

        temp = {
            'orders_placed': 0, 'orders_filled': 0, 'total_volume': 0.0,
            'errors': 0, 'warnings': 0, 'active_grids': set(),
            'positions': defaultdict(lambda: {'side': '', 'count': 0})
        }

        try:
            with open(self.log_file, 'r', errors='ignore') as f:
                lines = f.readlines()[-500:]

            for line in lines:
                line = line.strip()
                if not line:
                    continue
                    
                # Parse logfmt
                data = self.parse_logfmt(line)
                
                # Check for Volume Farming Metrics
                msg = data.get('msg', '')
                if 'Volume Farming Metrics' in msg:
                    try:
                        if 'total_volume_usdt' in data:
                            temp['total_volume'] = float(data['total_volume_usdt'])
                        if 'orders_placed' in data:
                            temp['orders_placed'] = int(data['orders_placed'])
                        if 'orders_filled' in data:
                            temp['orders_filled'] = int(data['orders_filled'])
                        if 'active_orders' in data:
                            temp['active_orders'] = int(data['active_orders'])
                    except:
                        pass
                
                # Check for Exchange Real Data (REAL data from exchange API)
                if 'Exchange Real Data' in msg:
                    try:
                        if 'exchange_open_orders' in data:
                            temp['exchange_orders'] = int(data['exchange_open_orders'])
                        if 'exchange_total_notional' in data:
                            temp['exchange_notional'] = float(data['exchange_total_notional'])
                        if 'exchange_positions_count' in data:
                            temp['exchange_positions'] = int(data['exchange_positions_count'])
                        # Parse positions list if available
                        if 'exchange_positions' in data:
                            try:
                                positions_str = data['exchange_positions']
                                if positions_str and positions_str != '[]':
                                    # Simple parsing for positions
                                    import json
                                    positions = json.loads(positions_str.replace("'", '"'))
                                    for pos in positions:
                                        sym = pos.get('symbol', '')
                                        if sym:
                                            temp['active_grids'].add(sym)
                                            side = pos.get('side', '')
                                            if side:
                                                temp['positions'][sym]['side'] = side
                                            # Use actual position size, not count
                                            size = pos.get('size', 0)
                                            notional = pos.get('notional', 0)
                                            temp['positions'][sym]['size'] = size
                                            temp['positions'][sym]['notional'] = notional
                                            # P/L data from exchange
                                            temp['positions'][sym]['unrealized_pnl'] = pos.get('unrealizedPnL', 0)
                                            temp['positions'][sym]['entry'] = pos.get('entry', 0)
                                            temp['positions'][sym]['mark'] = pos.get('mark', 0)
                            except:
                                pass
                        # Parse orders by symbol
                        if 'exchange_orders_by_symbol' in data:
                            try:
                                orders_str = data['exchange_orders_by_symbol']
                                if orders_str and orders_str != '{}':
                                    import json
                                    orders_by_sym = json.loads(orders_str.replace("'", '"'))
                                    for sym, count in orders_by_sym.items():
                                        if count > 0:
                                            temp['active_grids'].add(sym)
                            except:
                                pass
                    except:
                        pass
                
                # Extract symbols from various log messages
                sym = data.get('symbol', '')
                if sym:
                    temp['active_grids'].add(sym)
                    side = data.get('side', '')
                    if side:
                        temp['positions'][sym]['side'] = side
                        temp['positions'][sym]['count'] += 1
                
                # Spread metrics from grid_manager logs
                if 'spread_pct' in data:
                    try:
                        temp['spread_pct'] = float(data['spread_pct'])
                    except:
                        pass
                if 'dynamic_spread' in data:
                    try:
                        self.metrics['dynamic_spread'] = float(data['dynamic_spread'])
                    except:
                        pass
                if 'base_spread' in data:
                    try:
                        self.metrics['base_spread'] = float(data['base_spread'])
                    except:
                        pass
                
                # Check level for errors/warnings
                lvl = data.get('level', '').upper()
                if lvl == 'ERROR':
                    temp['errors'] += 1
                elif lvl in ('WARN', 'WARNING'):
                    temp['warnings'] += 1
                
                # Funding Rate metrics
                if 'Funding Rate Status' in msg:
                    try:
                        if 'funding_rate' in data:
                            self.metrics['funding_rate'] = float(data['funding_rate'])
                        if 'bias_side' in data:
                            self.metrics['funding_bias_side'] = data['bias_side']
                        if 'bias_strength' in data:
                            self.metrics['funding_bias_strength'] = float(data['bias_strength'])
                    except:
                        pass
                
                # Kelly Metrics
                if 'Kelly Metrics' in msg:
                    try:
                        if 'win_rate' in data:
                            self.metrics['win_rate'] = float(data['win_rate'])
                        if 'consecutive_losses' in data:
                            self.metrics['consecutive_losses'] = int(data['consecutive_losses'])
                        if 'total_trades' in data:
                            self.metrics['total_trades'] = int(data['total_trades'])
                    except:
                        pass
                
                # Inventory Metrics
                if 'Inventory Metrics' in msg:
                    try:
                        if 'skew_ratio' in data:
                            self.metrics['skew_ratio'] = float(data['skew_ratio'])
                        if 'net_exposure' in data:
                            self.metrics['net_exposure'] = float(data['net_exposure'])
                        if 'skew_action' in data:
                            self.metrics['skew_action'] = data['skew_action']
                    except:
                        pass

                # State Transitions
                if 'state_transition' in msg:
                    try:
                        symbol = data.get('symbol', '')
                        from_state = data.get('from_state', '')
                        to_state = data.get('to_state', '')
                        event = data.get('event', '')
                        if symbol and to_state:
                            # Update symbol state
                            if symbol not in self.symbol_states:
                                self.symbol_states[symbol] = {}
                            self.symbol_states[symbol]['state'] = to_state
                            self.symbol_states[symbol]['last_transition_event'] = event
                            # Add to transitions list (keep last 20)
                            self.state_transitions.insert(0, {
                                'symbol': symbol,
                                'from_state': from_state,
                                'to_state': to_state,
                                'event': event,
                                'time': datetime.now().strftime('%H:%M:%S')
                            })
                            if len(self.state_transitions) > 20:
                                self.state_transitions.pop()
                    except:
                        pass

                # Market Condition Evaluation
                if 'Market condition evaluation' in msg:
                    try:
                        symbol = data.get('symbol', '')
                        if symbol:
                            if symbol not in self.symbol_states:
                                self.symbol_states[symbol] = {}
                            self.symbol_states[symbol]['volatility_score'] = float(data.get('volatility_score', 0.5))
                            self.symbol_states[symbol]['trend_score'] = float(data.get('trend_score', 0.5))
                            self.symbol_states[symbol]['position_score'] = float(data.get('position_score', 0.5))
                            self.symbol_states[symbol]['risk_score'] = float(data.get('risk_score', 0.5))
                            self.symbol_states[symbol]['market_score'] = float(data.get('market_score', 0.5))
                            self.symbol_states[symbol]['recommended_state'] = data.get('recommended_state', '')
                            self.symbol_states[symbol]['confidence'] = float(data.get('confidence', 0.0))
                            self.symbol_states[symbol]['reason'] = data.get('reason', '')
                            self.symbol_states[symbol]['last_eval_time'] = datetime.now().strftime('%H:%M:%S')
                    except:
                        pass

                # Position Size Check
                if 'Position Size Check' in msg:
                    try:
                        symbol = data.get('symbol', '')
                        if symbol:
                            if symbol not in self.symbol_states:
                                self.symbol_states[symbol] = {}
                            self.symbol_states[symbol]['position_notional'] = float(data.get('position_notional', 0.0))
                            self.symbol_states[symbol]['threshold'] = float(data.get('threshold', 0.0))
                            self.symbol_states[symbol]['recovery_level'] = float(data.get('recovery_level', 0.0))
                    except:
                        pass

            self.metrics.update(temp)
            if temp['orders_placed'] > 0:
                self.metrics['fill_rate'] = (temp['orders_filled'] / temp['orders_placed']) * 100
            # active_orders from log is more accurate than calculated
            if 'active_orders' not in self.metrics or temp.get('active_orders', 0) > 0:
                self.metrics['active_orders'] = temp.get('active_orders', temp['orders_placed'] - temp['orders_filled'])
            self.metrics['positions'] = dict(temp['positions'])
            self.metrics['symbols'] = temp['active_grids']
            self.metrics['last_update'] = datetime.now().strftime('%H:%M:%S')
            self.metrics['status'] = 'RUNNING' if (temp['active_grids'] or temp['orders_placed'] > 0) else 'WAITING'
        except Exception as e:
            self.metrics['status'] = 'ERROR'

    def draw_box(self, y, x, h, w, title=""):
        self.stdscr.addch(y, x, curses.ACS_ULCORNER)
        self.stdscr.addch(y, x + w - 1, curses.ACS_URCORNER)
        self.stdscr.addch(y + h - 1, x, curses.ACS_LLCORNER)
        self.stdscr.addch(y + h - 1, x + w - 1, curses.ACS_LRCORNER)
        for i in range(1, w - 1):
            self.stdscr.addch(y, x + i, curses.ACS_HLINE)
            self.stdscr.addch(y + h - 1, x + i, curses.ACS_HLINE)
        for i in range(1, h - 1):
            self.stdscr.addch(y + i, x, curses.ACS_VLINE)
            self.stdscr.addch(y + i, x + w - 1, curses.ACS_VLINE)
        if title:
            t = f" {title} "
            self.stdscr.addstr(y, x + (w - len(t)) // 2, t, self.CYAN + self.BOLD)

    def draw_bar(self, y, x, w, val, maxv=100):
        filled = int((val / maxv) * (w - 2)) if maxv > 0 else 0
        filled = max(0, min(filled, w - 2))
        bar = "█" * filled + "░" * (w - 2 - filled)
        color = self.GREEN if val > 80 else (self.YELLOW if val > 50 else self.RED)
        self.stdscr.addstr(y, x, "[", self.WHITE)
        self.stdscr.addstr(y, x + 1, bar, color)
        self.stdscr.addstr(y, x + w - 1, "]", self.WHITE)

    def safe_addstr(self, y, x, text, attr=0):
        """Safely add string to screen, ignore if out of bounds"""
        try:
            h, w = self.stdscr.getmaxyx()
            if y < 0 or y >= h or x < 0 or x >= w:
                return
            if x + len(text) > w:
                text = text[:w - x - 1]
            self.stdscr.addstr(y, x, text, attr)
        except:
            pass

    def render(self):
        self.stdscr.clear()
        h, w = self.stdscr.getmaxyx()
        if h < 20 or w < 80:
            self.safe_addstr(0, 0, f"Terminal too small! Need 80x20, got {w}x{h}", self.RED)
            self.safe_addstr(1, 0, "Please resize your terminal and restart", self.YELLOW)
            self.stdscr.refresh()
            return

        t = " GRID TRADING DASHBOARD "
        self.stdscr.addstr(0, (w - len(t)) // 2, t, self.CYAN + self.BOLD)
        
        now = datetime.now().strftime('%Y-%m-%d %H:%M:%S')
        self.stdscr.addstr(1, 2, "Time: ", self.WHITE)
        self.stdscr.addstr(now, self.CYAN)
        self.stdscr.addstr(1, 30, "Status: ", self.WHITE)
        st = self.metrics['status']
        self.stdscr.addstr(f" {st} ", self.BG_GREEN if st == 'RUNNING' else (self.BG_RED if st == 'ERROR' else self.BG_YELLOW))
        self.stdscr.addstr(1, 50, f"Last Update: ", self.WHITE)
        self.stdscr.addstr(self.metrics['last_update'], self.CYAN)

        self.draw_box(3, 2, 10, 35, "METRICS (24h)")
        
        # Use real exchange data if available, otherwise fallback to internal tracking
        exchange_orders = self.metrics.get('exchange_orders', 0)
        exchange_notional = self.metrics.get('exchange_notional', 0)
        exchange_positions = self.metrics.get('exchange_positions', 0)
        
        # Orders: prefer exchange data
        orders_display = exchange_orders if exchange_orders > 0 else self.metrics['orders_placed']
        filled_display = self.metrics['orders_filled']
        
        self.stdscr.addstr(4, 4, "Volume:  ", self.WHITE)
        vol_display = exchange_notional if exchange_notional > 0 else self.metrics['total_volume']
        self.stdscr.addstr(f"${vol_display:,.2f}".rjust(20), self.GREEN + self.BOLD)
        
        self.stdscr.addstr(5, 4, "Orders:  ", self.WHITE)
        if exchange_orders > 0:
            self.stdscr.addstr(f"{exchange_orders} open (exch) / {filled_display} filled", self.CYAN + self.BOLD)
        else:
            self.stdscr.addstr(f"{orders_display} placed / {filled_display} filled", self.WHITE)
        
        self.stdscr.addstr(6, 4, "Fill:    ", self.WHITE)
        self.stdscr.addstr(f"{self.metrics['fill_rate']:.1f}%".rjust(20), self.GREEN)
        
        self.stdscr.addstr(7, 4, "Active:  ", self.WHITE)
        if exchange_orders > 0:
            self.stdscr.addstr(f"{exchange_orders} orders (exch)", self.CYAN + self.BOLD)
        else:
            self.stdscr.addstr(f"{self.metrics['active_orders']} orders", self.CYAN)
        
        # Show positions count from exchange if available
        if exchange_positions > 0:
            self.stdscr.addstr(8, 4, "Pos:     ", self.WHITE)
            self.stdscr.addstr(f"{exchange_positions} positions".rjust(20), self.YELLOW)
        # Additional metrics if available
        r = 8
        spread_val = self.metrics.get('spread_pct', 0)
        if spread_val > 0:
            self.stdscr.addstr(r, 4, "Spread:  ", self.WHITE)
            self.stdscr.addstr(f"{spread_val*100:.2f}%".rjust(20), self.CYAN)
            r += 1
        
        # Total Unrealized P/L (sum of all positions)
        total_pnl = 0
        for sym, p in self.metrics['positions'].items():
            total_pnl += p.get('unrealized_pnl', 0)
        if total_pnl != 0:
            self.stdscr.addstr(r, 4, "P/L:     ", self.WHITE)
            pnl_color = self.GREEN if total_pnl > 0 else self.RED
            self.stdscr.addstr(f"${total_pnl:,.2f}".rjust(20), pnl_color + self.BOLD)
            r += 1
        
        # Funding Rate (if available)
        funding_rate = self.metrics.get('funding_rate')
        if funding_rate is not None:
            self.safe_addstr(r, 4, "Funding: ", self.WHITE)
            bias_side = self.metrics.get('funding_bias_side', '')
            bias_str = f"{funding_rate:.4f}%"
            if bias_side:
                bias_str += f" ({bias_side})"
            self.safe_addstr(r, 4 + 9, bias_str.rjust(20), self.CYAN if abs(funding_rate) < 0.05 else self.YELLOW)
            r += 1
        
        # Kelly Metrics (if available)
        win_rate = self.metrics.get('win_rate')
        if win_rate is not None:
            self.safe_addstr(r, 4, "Kelly:   ", self.WHITE)
            cons_losses = self.metrics.get('consecutive_losses', 0)
            self.safe_addstr(r, 4 + 9, f"WR:{win_rate*100:.0f}% CL:{cons_losses}".rjust(20), self.CYAN)
            r += 1
        
        # Inventory Metrics (if available)
        skew_ratio = self.metrics.get('skew_ratio')
        if skew_ratio is not None:
            self.safe_addstr(r, 4, "Skew:    ", self.WHITE)
            skew_action = self.metrics.get('skew_action', '')
            skew_str = f"{skew_ratio:.2f}"
            if skew_action:
                skew_str += f" ({skew_action})"
            self.safe_addstr(r, 4 + 9, skew_str.rjust(20), self.CYAN if skew_ratio < 0.5 else self.YELLOW)

        self.draw_box(3, 38, 10, w - 40, "POSITIONS (Exchange)")
        pos = list(self.metrics['positions'].items())[:5]
        r = 4
        if pos:
            for sym, p in pos:
                color = self.GREEN if p.get('side') == 'BUY' else self.RED
                self.stdscr.addstr(r, 40, f"{sym:8}", self.CYAN)
                self.stdscr.addstr(f" {p.get('side', 'N/A'):6}", color)
                # Show real position size and notional
                size = p.get('size', 0)
                notional = p.get('notional', 0)
                pnl = p.get('unrealized_pnl', 0)
                if size > 0:
                    self.stdscr.addstr(f" {size:.4f}", self.WHITE)
                    self.stdscr.addstr(f" (${notional:,.2f})", self.YELLOW)
                    # Show P/L with color
                    pnl_color = self.GREEN if pnl >= 0 else self.RED
                    pnl_str = f" {'+' if pnl >= 0 else ''}${pnl:,.2f}"
                    self.stdscr.addstr(pnl_str, pnl_color + self.BOLD)
                else:
                    self.stdscr.addstr(f" x{p.get('count', 0)}", self.WHITE)
                r += 1
        else:
            self.stdscr.addstr(6, 45, "No active positions", self.YELLOW)

        gy = 14
        self.stdscr.addstr(gy, 2, "─" * (w - 4), self.WHITE)
        self.stdscr.addstr(gy + 1, 2, f" SYMBOL STATES ({len(self.symbol_states)})", self.CYAN + self.BOLD)
        r = gy + 3
        h, w = self.stdscr.getmaxyx()

        # Display symbol states with market conditions
        if self.symbol_states:
            for sym, state_data in list(self.symbol_states.items())[:5]:
                if r >= h - 2:
                    break
                state = state_data.get('state', 'UNKNOWN')
                # Color code states
                state_color = self.GREEN
                if state in ['OVER_SIZE', 'DEFENSIVE', 'EXIT_ALL']:
                    state_color = self.RED + self.BOLD
                elif state in ['EXIT_HALF', 'RECOVERY']:
                    state_color = self.YELLOW + self.BOLD
                elif state == 'TRADING':
                    state_color = self.BG_GREEN + self.BOLD
                elif state == 'IDLE':
                    state_color = self.CYAN

                try:
                    self.stdscr.addstr(r, 4, f"{sym:10}", self.CYAN + self.BOLD)
                    self.stdscr.addstr(r, 15, f"{state:12}", state_color)

                    # Show market condition scores if available
                    vol_score = state_data.get('volatility_score')
                    if vol_score is not None:
                        vol_color = self.RED if vol_score > 0.7 else (self.YELLOW if vol_score > 0.4 else self.GREEN)
                        self.stdscr.addstr(r, 28, f"V:{vol_score:.2f}", vol_color)

                    trend_score = state_data.get('trend_score')
                    if trend_score is not None:
                        trend_color = self.GREEN if trend_score > 0.6 else self.CYAN
                        self.stdscr.addstr(r, 35, f"T:{trend_score:.2f}", trend_color)

                    risk_score = state_data.get('risk_score')
                    if risk_score is not None:
                        risk_color = self.RED if risk_score > 0.7 else (self.YELLOW if risk_score > 0.4 else self.GREEN)
                        self.stdscr.addstr(r, 42, f"R:{risk_score:.2f}", risk_color)

                    # Show recommended state if different from current
                    rec_state = state_data.get('recommended_state', '')
                    if rec_state and rec_state != state:
                        conf = state_data.get('confidence', 0)
                        self.stdscr.addstr(r, 49, f"→{rec_state[:8]:8}", self.MAGENTA)
                        self.stdscr.addstr(r, 58, f"({conf:.0%})", self.CYAN)

                except curses.error:
                    pass
                r += 1
        else:
            if r < h:
                self.stdscr.addstr(r, 10, "Waiting for state data...", self.YELLOW)

        # Active grids section (simplified)
        gy = r + 2
        if gy < h - 5:
            self.stdscr.addstr(gy, 2, "─" * (w - 4), self.WHITE)
            self.stdscr.addstr(gy + 1, 2, f" ACTIVE GRIDS ({len(self.metrics['symbols'])})", self.CYAN + self.BOLD)
            r = gy + 3
            syms = list(self.metrics['symbols'])[:6]
            if syms:
                for sym in syms:
                    if r >= h - 2:
                        break
                    fp = self.metrics['fill_rate'] if self.metrics['fill_rate'] > 0 else 0
                    try:
                        self.stdscr.addstr(r, 4, f"{sym:8}", self.CYAN)
                        self.draw_bar(r, 15, 20, fp)
                        self.stdscr.addstr(r, 37, f"{fp:5.1f}%", self.WHITE)
                    except curses.error:
                        pass
                    r += 1
            else:
                if r < h:
                    self.stdscr.addstr(r, 10, "No active grids", self.YELLOW)

        # Recent state transitions
        gy = r + 2
        if gy < h - 5 and self.state_transitions:
            self.stdscr.addstr(gy, 2, "─" * (w - 4), self.WHITE)
            self.stdscr.addstr(gy + 1, 2, " RECENT STATE TRANSITIONS", self.CYAN + self.BOLD)
            r = gy + 3
            for trans in self.state_transitions[:4]:
                if r >= h - 2:
                    break
                try:
                    sym = trans['symbol']
                    from_state = trans['from_state']
                    to_state = trans['to_state']
                    event = trans['event']
                    time = trans['time']
                    self.stdscr.addstr(r, 4, f"{time}", self.CYAN)
                    self.stdscr.addstr(r, 10, f"{sym:10}", self.WHITE)
                    self.stdscr.addstr(r, 21, f"{from_state:12}", self.CYAN)
                    self.stdscr.addstr(r, 34, "→", self.WHITE)
                    self.stdscr.addstr(r, 36, f"{to_state:12}", self.GREEN + self.BOLD)
                    self.stdscr.addstr(r, 49, f"({event[:15]:15})", self.YELLOW)
                except curses.error:
                    pass
                r += 1

        ry = r + 2
        if ry < h - 3:
            self.stdscr.addstr(ry, 2, "─" * (w - 4), self.WHITE)
            self.stdscr.addstr(ry + 1, 2, " RISK", self.CYAN + self.BOLD)
            r = ry + 2
            risk_status = "GREEN ✓" if self.metrics['errors'] == 0 and self.metrics['warnings'] < 5 else ("YELLOW ⚠" if self.metrics['errors'] < 5 else "RED ✗")
            risk_color = self.BG_GREEN if self.metrics['errors'] == 0 and self.metrics['warnings'] < 5 else (self.BG_YELLOW if self.metrics['errors'] < 5 else self.BG_RED)
            self.stdscr.addstr(r, 4, f"Status: {risk_status} ", risk_color)
            r += 1
            self.stdscr.addstr(r, 4, f"Errors: {self.metrics['errors']}  Warnings: {self.metrics['warnings']}", 
                              self.WHITE)

        self.stdscr.addstr(h - 1, (w - 25) // 2, " Press Q or Ctrl+C to exit ", self.MAGENTA + self.BOLD)
        self.stdscr.refresh()

    def run(self):
        while self.running:
            self.parse_log_file()
            self.render()
            self.stdscr.timeout(1000)
            try:
                key = self.stdscr.getch()
                if key in (ord('q'), ord('Q'), 27):
                    self.running = False
            except:
                pass

def find_log():
    for p in ['volume-farm.log', 'logs/volume-farm.log', '../logs/volume-farm.log']:
        if os.path.exists(p):
            return p
    return os.environ.get('BOT_LOG_FILE')

def main():
    lf = find_log()
    if not lf:
        print("Error: Cannot find log file.")
        sys.exit(1)
    curses.wrapper(lambda s: DashboardApp(s, lf).run())

if __name__ == '__main__':
    main()
