#!/usr/bin/env python3
"""Simple Grid Trading Dashboard - Termux Compatible"""

import os
import re
import sys
import time
from datetime import datetime
from collections import defaultdict

def clear_screen():
    os.system('clear' if os.name != 'nt' else 'cls')

def find_log():
    for p in ['volume-farm.log', 'logs/volume-farm.log', '../logs/volume-farm.log']:
        if os.path.exists(p):
            return p
    return os.environ.get('BOT_LOG_FILE')

def parse_metrics(log_file):
    metrics = {
        'total_volume': 0.0, 'orders_placed': 0, 'orders_filled': 0,
        'fill_rate': 0.0, 'active_orders': 0, 'errors': 0, 'warnings': 0,
        'status': 'WAITING', 'last_update': '', 'symbols': set()
    }
    
    try:
        with open(log_file, 'r') as f:
            lines = f.readlines()
            for line in lines[-500:]:  # Last 500 lines
                # Volume
                if 'total_volume_usdt=' in line:
                    m = re.search(r'total_volume_usdt=([\d.]+)', line)
                    if m:
                        metrics['total_volume'] = float(m.group(1))
                
                # Orders placed
                if 'orders_placed=' in line:
                    m = re.search(r'orders_placed=(\d+)', line)
                    if m:
                        metrics['orders_placed'] = int(m.group(1))
                
                # Orders filled
                if 'orders_filled=' in line:
                    m = re.search(r'orders_filled=(\d+)', line)
                    if m:
                        metrics['orders_filled'] = int(m.group(1))
                
                # Active orders
                if 'active_orders=' in line:
                    m = re.search(r'active_orders=(\d+)', line)
                    if m:
                        metrics['active_orders'] = int(m.group(1))
                
                # Fill rate
                if 'fill_rate=' in line:
                    m = re.search(r'fill_rate="([\d.]+)%"', line)
                    if m:
                        metrics['fill_rate'] = float(m.group(1))
                
                # Status
                if 'Status:' in line and 'RUNNING' in line:
                    metrics['status'] = 'RUNNING'
                
                # Errors
                if 'level=error' in line:
                    metrics['errors'] += 1
                
                # Warnings
                if 'level=warning' in line:
                    metrics['warnings'] += 1
                
                # Symbols
                if 'symbol=' in line:
                    m = re.search(r'symbol=(\w+)', line)
                    if m:
                        metrics['symbols'].add(m.group(1))
                
                # Last update
                if 'time=' in line:
                    m = re.search(r'time="([^"]+)"', line)
                    if m:
                        metrics['last_update'] = m.group(1)
    except Exception as e:
        metrics['status'] = f'ERROR: {str(e)[:30]}'
    
    return metrics

def draw_bar(val, width=20):
    filled = int((val / 100) * width) if val > 0 else 0
    filled = max(0, min(filled, width))
    return '█' * filled + '░' * (width - filled)

def main():
    log_file = find_log()
    if not log_file:
        print("Error: Cannot find log file (volume-farm.log)")
        sys.exit(1)
    
    print(f"Monitoring: {log_file}")
    print("Press Ctrl+C to exit\n")
    time.sleep(1)
    
    try:
        while True:
            metrics = parse_metrics(log_file)
            clear_screen()
            
            now = datetime.now().strftime('%H:%M:%S')
            status = metrics['status']
            status_icon = '✓' if status == 'RUNNING' else '✗' if 'ERROR' in status else '○'
            
            # Compact output for small terminals
            print(f"═══ GRID DASHBOARD [{now}] ═══")
            print(f"Status: {status_icon} {status}")
            print(f"Volume: ${metrics['total_volume']:,.2f}")
            print(f"Orders: {metrics['orders_placed']} placed / {metrics['orders_filled']} filled")
            print(f"Fill:   {draw_bar(metrics['fill_rate'])} {metrics['fill_rate']:.1f}%")
            print(f"Active: {metrics['active_orders']} orders")
            print(f"Errors: {metrics['errors']} | Warn: {metrics['warnings']}")
            print(f"Symbols: {len(metrics['symbols'])}")
            if metrics['symbols']:
                symbols_list = list(metrics['symbols'])[:5]
                print(f"  {', '.join(symbols_list)}")
            print(f"═══════════════════════════════")
            print("Q+Enter or Ctrl+C to exit")
            
            # Check for quit
            try:
                import select
                import sys
                if select.select([sys.stdin], [], [], 0)[0]:
                    key = sys.stdin.read(1)
                    if key.lower() == 'q':
                        break
            except:
                pass
            
            time.sleep(2)
            
    except KeyboardInterrupt:
        print("\nExiting...")

if __name__ == '__main__':
    main()
