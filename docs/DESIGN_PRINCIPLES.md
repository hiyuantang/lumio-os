# Lumio OS — Design Principles

## Feeling

1. The macOS inspiration is **behavioral, never visual copying**. The
   feeling comes from calm interaction, hierarchy, consistency and
   animation — not from reproducing Apple assets pixel-for-pixel.
2. The desktop is calm: a real desktop, not a dashboard. Motion and
   color exist to explain state, never to decorate.
3. Hierarchy is explicit: the menu bar, windows, dock and notification
   center each have one clear role and one clear visual level.
4. Consistency is binding: the same action looks, behaves and is named
   the same way in every application.

## Originality rules

Lumio OS ships its own product name and logo, wallpaper, color system,
icon set, typography, window controls, sound and animation language, and
application names. The following rules are binding for every UI task:

1. **UI font:** Inter (SIL OFL) with a system-ui fallback stack. Never
   San Francisco, never SF Symbols.
2. **Window controls:** left-aligned with original geometry — rounded
   squares carrying glyphs. Never macOS traffic-light circles.
3. **Accent palette:** warm teal plus amber. Never macOS blue.
4. **Icons:** original inline SVG in a geometric line style. Never Apple
   assets, never third-party icon packs copied verbatim.

## Interaction rules

1. All shell interaction renders locally in the browser with **zero
   server round trips**: moving a window, opening a menu or switching
   applications never touches the network.
2. Motion is 120–200 ms ease-out. Nothing bounces, nothing lingers.
3. Every destructive action confirms before it executes.

## Accessibility

1. All key operations are reachable by keyboard.
2. Focus is always visible.
3. ARIA roles are applied to windows, the dock and menus.
4. `prefers-reduced-motion` is honored, and a manual reduced-motion
   setting is provided alongside it.
5. Light and dark themes both meet WCAG AA contrast.
