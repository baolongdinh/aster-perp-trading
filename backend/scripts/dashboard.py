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
    def __init__(self, stdscr, log_file):
        self.stdscr = stdscr
        self.log_file = log_file
        self.running = True
        self.metrics = {
            'total_volume': 0.0, 'orders_placed': 0, 'orders_filled': 0,
            'fill_rate': 0.0, 'active_orders': 0, 'positions': {},
            'symbols': set(), 'last_update': '', 'errors': 0, 'warnings': 0,
            'status': 'WAITING'
        }
        
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
        if h < 10 or w < 30:
            self.safe_addstr(0, 0, "Terminal too small! Need 30x10+", self.RED)
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

        self.draw_box(3, 2, 8, 35, "METRICS (24h)")
        self.stdscr.addstr(4, 4, "Volume:  ", self.WHITE)
        self.stdscr.addstr(f"${self.metrics['total_volume']:,.2f}".rjust(20), self.GREEN + self.BOLD)
        self.stdscr.addstr(5, 4, "Orders:  ", self.WHITE)
        self.stdscr.addstr(f"{self.metrics['orders_placed']} placed / {self.metrics['orders_filled']} filled", self.WHITE)
        self.stdscr.addstr(6, 4, "Fill:    ", self.WHITE)
        self.stdscr.addstr(f"{self.metrics['fill_rate']:.1f}%".rjust(20), self.GREEN)
        self.stdscr.addstr(7, 4, "Active:  ", self.WHITE)
        self.stdscr.addstr(f"{self.metrics['active_orders']} orders", self.CYAN)
        # Spread (if available)
        spread_val = self.metrics.get('spread_pct', 0)
        if spread_val > 0:
            self.stdscr.addstr(8, 4, "Spread:  ", self.WHITE)
            self.stdscr.addstr(f"{spread_val*100:.2f}%".rjust(20), self.CYAN)

        self.draw_box(3, 38, 8, w - 40, "POSITIONS")
        pos = list(self.metrics['positions'].items())[:5]
        r = 4
        if pos:
            for sym, p in pos:
                color = self.GREEN if p.get('side') == 'BUY' else self.RED
                self.stdscr.addstr(r, 40, f"{sym:8}", self.CYAN)
                self.stdscr.addstr(f" {p.get('side', 'N/A'):6}", color)
                self.stdscr.addstr(f" x{p.get('count', 0)}", self.WHITE)
                r += 1
        else:
            self.stdscr.addstr(6, 45, "No active positions", self.YELLOW)

        gy = 14
        self.stdscr.addstr(gy, 2, "─" * (w - 4), self.WHITE)
        self.stdscr.addstr(gy + 1, 2, f" ACTIVE GRIDS ({len(self.metrics['symbols'])})", self.CYAN + self.BOLD)
        syms = list(self.metrics['symbols'])[:6]
        r = gy + 3
        if syms:
            for sym in syms:
                fp = self.metrics['fill_rate'] if self.metrics['fill_rate'] > 0 else 0
                self.stdscr.addstr(r, 4, f"{sym:8}", self.CYAN)
                self.draw_bar(r, 15, 25, fp)
                self.stdscr.addstr(r, 42, f"{fp:5.1f}%", self.WHITE)
                r += 1
        else:
            self.stdscr.addstr(r, 10, "Waiting for data...", self.YELLOW)

        ry = gy + 10
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
