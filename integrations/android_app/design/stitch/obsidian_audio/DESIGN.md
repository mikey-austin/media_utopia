# Design System Documentation: The Sonic Curator

## 1. Overview & Creative North Star
The Creative North Star for this design system is **"The Sonic Curator."** 

We are not designing a standard utility app; we are crafting a digital instrument that mirrors the tactile precision of high-end, vacuum-tube amplifiers and precision-milled aluminum audio gear. To achieve this, the UI must embrace "The Silence Between the Notes"—leveraging expansive negative space, intentional asymmetry, and a radical rejection of traditional "web" containers. 

This system breaks the "template" look by using **Tonal Depth** instead of lines. We treat the interface as a series of physical layers, where hierarchy is communicated through light and texture rather than borders and boxes. The result is an editorial, high-contrast experience that feels both authoritative and invisible, allowing the album art and the music to remain the primary subjects.

---

## 2. Colors & Surface Philosophy
The palette is rooted in deep obsidian and charcoal tones, punctuated by high-energy Amber and Teal accents that mimic the glow of a status LED on a high-end DAC.

### The "No-Line" Rule
**Explicit Instruction:** Designers are prohibited from using 1px solid borders to section content. Layout boundaries must be defined solely through background color shifts. 
- Use `surface_container_low` for secondary sections sitting on a `surface` background.
- Use `surface_container_high` to define high-priority interaction zones.
- This creates a seamless, "molded" look common in premium industrial design.

### Surface Hierarchy & Nesting
Treat the UI as a series of stacked materials. Use the `surface-container` tiers to create organic depth:
*   **Base Layer:** `surface` (#131313) or `surface_container_lowest` (#0e0e0e) for the primary application background.
*   **Nesting:** Place a `surface_container_low` card on a `surface` background to create a soft, natural lift. For tertiary elements within that card, use `surface_variant`.

### The Glass & Signature Textures
To move beyond a flat "Material" feel, use **Glassmorphism** for floating elements (like a Now Playing bar or a persistent volume slider).
- **Token:** Use `surface_variant` at 60-80% opacity with a `20px` to `40px` backdrop-blur.
- **CTAs:** For primary actions, use a subtle linear gradient transitioning from `primary` (#ffe2ab) to `primary_container` (#ffbf00) at a 135-degree angle. This provides a "lathed metal" soul that flat colors cannot replicate.

---

## 3. Typography: Precision & Scale
We utilize a high-contrast typography scale to create an editorial feel. The choice of **Inter** (or **Mona Sans**) provides a technical, Swiss-style precision.

*   **Display (lg/md):** Reserved for track titles and immersive headers. Use these sparingly to create "hero" moments.
*   **Headline (sm/md):** Used for section headers (e.g., "Recently Played"). These should be high-contrast (`on_surface`) to ensure immediate scannability.
*   **Body (lg/md):** Our workhorse. Ensure `body-md` is used for all metadata (Artist names, Album years) using the `on_surface_variant` token to create a clear hierarchy.
*   **Labels:** Use `label-sm` in all-caps with `0.05rem` letter spacing for technical data (Sample rates, Bit depth, File format) to mimic the engraving on audio hardware.

---

## 4. Elevation & Depth
In this system, elevation is an atmospheric property, not a structural one.

### Tonal Layering
Depth is achieved by "stacking" surface tokens. For example, a track list might sit on `surface_container_low`, while the individual "active" track item shifts to `surface_container_highest`.

### Ambient Shadows
Avoid traditional "Drop Shadows." When an element must float (e.g., a Context Menu):
- **Shadow Color:** Use a tinted version of `on_surface` at 6% opacity.
- **Blur:** Use extra-diffused values (e.g., `32px` blur, `8px` Y-offset). It should feel like a soft glow of light being blocked, not a dark smudge.

### The "Ghost Border" Fallback
If a border is required for extreme accessibility needs:
- **Rule:** Use `outline_variant` at 15% opacity. Never use 100% opaque borders. The goal is a "hint" of a boundary, invisible until the eye seeks it.

---

## 5. Components

### Buttons (The "Control" Set)
- **Primary:** Gradient fill (`primary` to `primary_container`). `0.25rem` (sm) roundedness. No border. Text in `on_primary`.
- **Secondary:** Surface-only. Background: `surface_container_highest`. Text: `primary`.
- **Tertiary:** Text-only. Use `primary` for the label.

### Audiophile Specialized Components
- **Playback Scrubber:** Use `primary` for the "played" track and `surface_container_highest` for the "unplayed" track. The "thumb" should be a minimal `0.75rem` circle that only appears on hover/touch.
- **Metadata Badges:** Use `surface_variant` background with `label-sm` typography for "FLAC," "HI-RES," or "24-BIT."

### Cards & Lists
- **Rule:** Forbid divider lines. Separate list items using `12px` of vertical white space or a `2px` background color shift on hover.
- **Asymmetry:** In album grids, use varied aspect ratios (e.g., a large featured album card next to a vertical list of top tracks) to break the "standard grid" monotony.

### Input Fields
- **State:** Background: `surface_container_low`. Bottom-border only (using `outline_variant` at 30% opacity). When focused, the border transitions to `secondary` (#76d6d5).

---

## 6. Do's and Don'ts

### Do
- **Do** prioritize "negative space" as a functional element to reduce cognitive load during a listening session.
- **Do** use `secondary` (Teal) for "active" states (e.g., an active speaker icon or a toggled-on Shuffle button).
- **Do** use high-quality, large-scale imagery. Let the album art bleed into the background using a subtle gradient overlay.

### Don't
- **Don't** use pure white (#FFFFFF) for body text; use `on_surface` (#e5e2e1) to prevent eye strain in dark environments.
- **Don't** use rounded corners larger than `0.75rem` (xl). We want the system to feel architectural and precise, not "bubbly" or "playful."
- **Don't** use standard Material icons if a more minimal, thin-stroke alternative is available. Keep icon weights consistent with typography weights.

---
**Director's Note:** This system is about the tension between the dark void of the background and the surgical precision of the typography. If it feels like a standard app, you've added too many lines. If it feels like a piece of high-end equipment, you've succeeded.