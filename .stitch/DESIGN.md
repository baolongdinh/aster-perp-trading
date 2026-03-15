# Design System: Aster Perp Trading Dashboard
**Project ID:** 12066261183802566974

## 1. Visual Theme & Atmosphere
- **Mood**: Sophisticated, Utilitarian, Professional, High-Tech.
- **Density**: High (Analyst-grade), with efficient use of whitespace.
- **Aesthetic**: Modern Dark Mode with glassmorphism elements and glowing accents.

## 2. Color Palette & Roles
- **Deep Slate Background (#0b0e11)**: Primary page background.
- **Surface Gray (#151a1e)**: Card and container backgrounds.
- **Aster Cyan Accent (#40baf7)**: Primary actions, active toggles, and positive metrics.
- **Danger Red (#f84960)**: Negative P&L, sell orders, and critical alerts.
- **Success Green (#0ecb81)**: Positive P&L, buy orders, and success states.
- **Soft Text Gray (#848e9c)**: Secondary labels and muted text.
- **Pure White (#ffffff)**: Headings and primary values.

## 3. Typography Rules
- **Font**: Inter (Sans-serif) for all UI.
- **Headings**: Semi-bold, large (text-2xl), white.
- **Metrics**: Monospaced numbers for alignment in tables and cards.

## 4. Component Stylings
* **Buttons**: Sharp borders or subtly rounded (4px). Glow on hover.
* **Cards/Containers**: Thin 1px border (#2b3139), subtle backdrop blur, zero or whisper-soft shadows.
* **Inputs/Forms**: Dark background (#0b0e11), cyan focus ring.

## 5. Layout Principles
- **Grid Layout**: Dashboard uses a CSS Grid for modularity.
- **Margins**: Tight margins (16px) to maximize data density.
- **Consistency**: All screens follow a sidebar-navigation layout.

## 6. Design System Notes for Stitch Generation
[Use the atmosphere and colors defined above for consistent generations.]
- Always use high-contrast dark mode.
- Use glows for active elements.
- Ensure all charts are embedded in styled cards.
