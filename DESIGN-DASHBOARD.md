# Design System Strategy: The Kinetic Precision Framework

## 1. Overview & Creative North Star
**The Creative North Star: "The Financial Observatory"**

This design system moves away from the cluttered, anxiety-inducing grids of traditional trading platforms. Instead, it adopts the persona of a high-end observatory: a calm, dark environment where data is illuminated like celestial bodies. 

We break the "template" look through **Tonal Architecture**. By using the depth of the dark blue spectrum (`#10141a`) and intentional asymmetry, we guide the trader's eye toward critical movements without visual noise. The layout relies on "breathing" data—significant white space (or "dark space") that allows numbers to feel prestigious and intentional rather than cramped.

## 2. Color Philosophy: Depth Over Definition
In this system, color is not just decoration; it is a functional layer of the UI.

### The "No-Line" Rule
To achieve a premium, editorial feel, **1px solid borders are prohibited for sectioning.** Conventional lines create visual "stutter." Instead, boundaries must be defined by:
*   **Background Shifts:** Place a `surface_container_low` section directly against a `surface` background.
*   **Tonal Transitions:** Use a soft gradient transition between `surface_container` and `surface_container_high` to imply a new zone.

### Surface Hierarchy & Nesting
Treat the dashboard as a series of physical layers—stacked sheets of tinted obsidian.
*   **Base:** `surface` (#10141a)
*   **Primary Workspaces:** `surface_container_low` (#181c22)
*   **Active Interaction Cards:** `surface_container` (#1c2026)
*   **Pop-overs/Modals:** `surface_bright` (#353940)

### The "Glass & Gradient" Rule
For high-frequency trading components (like the "Buy/Sell" quick-action panel), use **Glassmorphism**. Apply `surface_variant` at 60% opacity with a `24px` backdrop-blur. This keeps the trader grounded in the global market context while focusing on a specific action.

### Signature Textures
Main CTAs (e.g., "Execute Trade") should utilize a linear gradient from `primary` (#b7c4ff) to `primary_container` (#0052ff) at a 135-degree angle. This adds a "jewel-like" depth that distinguishes the action from static data.

## 3. Typography: The Editorial Edge
We utilize a dual-font strategy to balance authority with technical precision.

*   **Display & Headlines (Manrope):** Chosen for its modern, wide stance. Use `display-lg` and `headline-md` for portfolio balances and high-level market caps. This creates a "Financial Magazine" aesthetic.
*   **Data & Body (Inter):** A workhorse for legibility. All ticker symbols, price points, and timestamps use `body-md` or `label-sm`. Inter’s tall x-height ensures that even at small sizes, financial digits are unmistakable.

**Hierarchy Tip:** Always pair a `display-sm` balance amount with a `label-md` muted text (using `on_surface_variant`) to provide immediate context without competing for attention.

## 4. Elevation & Depth
We eschew traditional drop shadows for **Tonal Layering**.

*   **The Layering Principle:** Depth is achieved by "stacking." Place a `surface_container_highest` element on top of a `surface_container_low` background to create a natural, "soft lift."
*   **Ambient Shadows:** If a card must float (e.g., a draggable widget), use an extra-diffused shadow: `offset-y: 12px, blur: 32px, color: rgba(0, 0, 0, 0.4)`. Never use pure black; always tint the shadow with the background hue.
*   **The "Ghost Border" Fallback:** If a border is required for accessibility, use `outline_variant` (#434656) at **15% opacity**. It should be felt, not seen.
*   **Refractive Light:** On active states, apply a 1px top-stroke (inner shadow) using `primary` at 20% opacity to mimic light hitting the edge of a glass pane.

## 5. Components

### Trading Buttons
*   **Primary (Buy):** Gradient from `secondary` (#44e092) to `secondary_container` (#03c177). High-gloss finish.
*   **Primary (Sell):** Gradient from `tertiary` (#ffb4aa) to `tertiary_container` (#cc0d11).
*   **Secondary/Neutral:** `surface_container_highest` with `on_surface` text.

### The "Value Chip"
Used for percentage changes. 
*   **Profit:** `secondary_fixed_dim` text on a `on_secondary_container` background.
*   **Loss:** `tertiary` text on a `on_tertiary_container` background.
*   **Shape:** Use `md` (0.375rem) corner radius for a technical look.

### Input Fields
Avoid "box" inputs. Use a bottom-only border of `outline_variant` that transitions to a 2px `primary` border on focus. Use `surface_container_lowest` as the fill color to "recede" into the dashboard.

### Cards & Data Tables
**Forbid the use of divider lines.**
*   **Separation:** Use vertical white space (16px, 24px, or 32px) to group data.
*   **Alternating Rows:** For large ledgers, use a subtle background shift to `surface_container_low` for every second row.

### Trade Status Indicators
Use the `full` (9999px) roundedness scale for status pips. A "Live" connection should use `secondary` with a soft "breathing" glow effect (blur: 8px).

## 6. Do's and Don'ts

### Do:
*   **Do** use `on_surface_variant` (#c3c5d9) for all non-essential labels to maintain a high-contrast ratio for the actual financial data.
*   **Do** use `xl` (0.75rem) roundedness for main dashboard containers to soften the technical edge.
*   **Do** allow charts to "bleed" to the edge of their containers to maximize data visualization real estate.

### Don't:
*   **Don't** use pure white (#FFFFFF). It causes eye strain in dark financial environments. Use `on_surface` (#dfe2eb) instead.
*   **Don't** use standard "Drop Shadows" on cards. Use background tonal shifts to define the grid.
*   **Don't** use "Success Green" for anything other than profit or positive actions. Using it for "Active" or "Online" can confuse the trader's mental model of their P&L.