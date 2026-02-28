# OpenUsage Landing Redesign Brief (Factory-leaning)

Last updated: 2026-02-28

## Objective

Design a minimal, high-credibility landing page that feels closer to Factory/OpenCode than typical SaaS marketing pages.

Primary outcome:
- user immediately understands what OpenUsage does
- user can install in one click path
- visual tone signals "serious engineering tool", not "consumer/candy"

## Visual Direction

Style name: Industrial editorial minimal

Guidelines:
- mostly neutral palette, single restrained accent
- strong typography contrast: large geometric sans headline + mono UI labels
- thin borders, square corners, low/no shadows
- sparse layout with large quiet areas
- subtle texture only (very light grid/diagonal lines), never dominant

Avoid:
- saturated gradients
- pill-heavy playful UI
- dense feature-card grids
- oversized animated decorations

## Information Architecture (Less-is-more)

Sections:
1. Top nav
2. Hero (copy + install + product visual)
3. Optional trust strip (small, grayscale logos only)
4. Footer

Do not include:
- feature matrix
- long "how it works"
- testimonials block
- pricing cards

## Hero Requirements

Left column:
- eyebrow line: "OpenUsage / Local telemetry runtime"
- H1: "Know what your agents cost."
- short subcopy (one sentence)
- primary CTA: Install
- secondary CTA: GitHub
- OS tabs (macOS/Linux, Windows) + copyable install command
- one short proof line: "No cloud account. No telemetry lock-in. Just local data."

Right column:
- single product artifact (dashboard screenshot)
- framed like a terminal/app shell
- static image first; optional subtle reveal animation later

## Content Tone

- short, factual, technical
- no hype adjectives
- no claim stacking
- one idea per line

## Token Design System

Use variables only:
- `--bg`: warm light gray
- `--paper`: slightly lighter panel background
- `--ink`: near-black
- `--muted`: medium gray text
- `--line`: subtle border
- `--line-strong`: stronger border
- `--accent`: muted orange (single accent)

Typography:
- Headline/body: IBM Plex Sans
- UI/meta/code: IBM Plex Mono

## Component Rules

Navbar:
- compact height, simple links, one dark CTA

Buttons:
- square corners
- uppercase mono labels
- 2 variants only (solid/ghost)

Install card:
- bordered panel
- OS tabs as tiny outlined chips
- mono command line + copy button

Preview shell:
- black panel with thin border
- mono header text
- one status dot using accent color

## Responsive Behavior

Desktop:
- 55/45 split hero layout

Tablet:
- reduce gaps and heading scale

Mobile:
- single column
- copy first, preview second
- command line scrolls horizontally

## Motion

Allowed:
- short fade-up on hero content and preview
- button hover color transitions

Not allowed:
- looping hero animation
- parallax
- decorative motion not tied to information

## Inspiration References (Captured)

Core:
- https://factory.ai
- https://opencode.ai
- https://openhands.dev

Adjacents:
- https://cursor.com
- https://windsurf.com
- https://linear.app
- https://railway.com
- https://warp.dev
- https://aider.chat

Local screenshots:
- `.tmp/inspiration-shots/factory-home-fresh.png`
- `.tmp/inspiration-shots/opencode-home-fresh.png`
- `.tmp/inspiration-shots/openhands-home-fresh.png`
- `.tmp/inspiration-shots/cursor-home-fresh.png`
- `.tmp/inspiration-shots/windsurf-home-fresh.png`
- `.tmp/inspiration-shots/linear-home-fresh.png`
- `.tmp/inspiration-shots/railway-home-fresh.png`
- `.tmp/inspiration-shots/warp-home-fresh.png`
- `.tmp/inspiration-shots/aider-home-fresh.png`

## Implementation Scope (Next Pass)

1. tighten spacing/scale and remove any remaining decorative excess
2. reduce top-nav noise
3. keep only hero + footer (+ optional trust strip)
4. refine install command block hierarchy
5. validate mobile layout and visual balance
