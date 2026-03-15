---
page: config
---
A high-fidelity "Analyst-Grade" Global Configuration (Settings) screen for the Aster Perp Trading Bot. This page manages API keys, security settings, and global risk management parameters.

**DESIGN SYSTEM (REQUIRED):**
- Platform: Web, Desktop-first
- Theme: Sophisticated Dark Mode, Utilitarian, Glassmorphic
- Atmosphere: Professional and High-Tech
- Background: Deep Slate Background (#0b0e11)
- Surface: Surface Gray (#151a1e) with thin 1px border (#2b3139)
- Primary Accent: Aster Cyan (#40baf7) for primary indicators
- Success: Success Green (#0ecb81)
- Danger: Danger Red (#f84960)
- Typography: Inter font family, monospaced numbers for metrics
- Layout: Sidebar navigation on the left, main content grid on the right

**Page Structure:**
1. **Header:** Title "Global Configuration" and a "Save All Changes" floating action button.
2. **Sections (Cards):**
    - **API Connectivity**: Fields for ASTER_USER (Wallet Address) and ASTER_SIGNER (Signer Address). A "Test Connection" button with status indicator. (Note: Private keys are NEVER shown, only a "Update Key" secure field).
    - **Global Risk Limits**:
        - Max Total Positions (Input)
        - Global Stop Loss % (Slider)
        - Daily Drawdown Cutoff (Input)
        - Minimum Equity Protection (Input)
    - **Notification Settings**: Webhook URL field for Discord/Telegram notifications, and toggle switches for different event types (Fills, Liquidations, Errors).
    - **System Info**: Read-only display of Build Version, Uptime, and Node.js/Go Environment status.

**Visual Polish:**
- Use glassmorphism for settings containers.
- Clearly differentiate between secure/sensitive inputs and standard ones.
- Include a "Danger Zone" section with a red border for sensitive actions (e.g., Reset Bot Data).
- Maintain consistency with the Dashboard and Strategy layouts.
