# Village Sprite Brief — Nano Banana Prompt Sheet

**Use.** Paste each section into Gemini (AI Ultra, Nano Banana / Imagen image mode) in
order. Run the master style sheet *first* and keep the output open while generating
per-building sprites so you can attach it as a reference image for consistency.

**Deliverable.** Per building: four state variants (Healthy, Degraded, Critical, Offline)
at 1024×1024 PNG with transparent background. Enterprise-manor variants are generated per
named customer later, outside this brief.

---

## Stage 1 — Master Style Sheet (run first)

Run this prompt **once** and keep the output. Attach it as a reference image to every
subsequent generation to hold style.

> Create a master art style sheet for a stylized low-poly isometric village monitoring UI.
> Flat illustrative rendering with warm hand-painted finish — a cross between Monument
> Valley and Townscaper. Camera: 3/4 isometric view, 30° pitch, orthographic. Single warm
> sun light from upper-left; soft long shadows to the south-east. Muted palette: warm sand,
> weathered stone, timber, moss, slate-grey, cold iron, banked ember. No heavy outlines;
> gentle ambient occlusion. Transparent background. Include on one sheet: base ground
> tile (1×1), wall segment, small villager silhouette, small tent, small house, medium
> building silhouette, large named manor silhouette. No text, no logos, no characters with
> faces.

Save this image as `style-sheet.png` in your working folder.

---

## Stage 2 — Per-building Prompts

For every building below, use this template, filling in the bracketed fields. **Attach
`style-sheet.png` as a reference image** and select "match style / composition" in Gemini.

```
Generate an isometric building sprite matching the attached style sheet exactly in palette,
lighting, camera angle, and rendering. The building is: [BUILDING DESCRIPTION]. Footprint:
[1×1 | 2×2 | 3×3] tile. District palette: [Gate / Town / Keep]. State: Healthy (default
animations implied, no warning signals). No text, no logos, no characters with faces.
Transparent background. 1024×1024.
```

Generate the **Healthy** variant first. Then generate three more variants by appending the
following paragraph to the same prompt, replacing the state descriptor:

- **Degraded** — *"State: Degraded. Add a thin amber smoke plume rising from one point on
  the building; small amber warning pennant on the highest point. Everything else identical."*
- **Critical** — *"State: Critical. Visible flames licking from one corner; structure
  leaning slightly; a few scattered debris pieces at the base. Palette shifted cooler with
  red highlights."*
- **Offline / Ruined** — *"State: Offline. The building is a quiet ruin: partially
  collapsed walls, no smoke, no light. Overgrown with small patches of moss. Crows perched
  on what remains."*

### Gate District (warm sand / stone / timber)

| Slug             | Footprint | Description                                                                                                           |
|------------------|-----------|-----------------------------------------------------------------------------------------------------------------------|
| `the-gate`       | 3×3       | A fortified stone gatehouse with twin towers and a central raised portcullis. Two small guard figures standing watch. |
| `heralds-stage`  | 1×1       | A small round stage of polished wood with a canopy awning and a banner post (no banner drawn).                        |
| `customs-house`  | 2×2       | A timber-and-stone counting office with a wide open doorway, stacks of sealed crates and rope-tied packages outside.  |
| `letter-slot`    | 1×1       | A squat stone postbox-shaped building with a slot on its face and a small awning.                                     |
| `tollbooth`      | 1×1       | A tiny hexagonal kiosk of brass-bound timber with a visible coin chute on its side.                                   |
| `code-forge`     | 2×2       | A blacksmith's forge with an open front, visible anvil, and a stone chimney; timber roof.                             |
| `watchtower`     | 1×1       | A tall thin stone tower with a small railing at the top and a telescope pointed skyward.                              |

### Town Proper (neutral earth / moss / slate-grey)

| Slug                   | Footprint | Description                                                                                                    |
|------------------------|-----------|----------------------------------------------------------------------------------------------------------------|
| `town-hall`            | 3×3       | A stately civic hall of pale stone with a tall bell tower, arched windows, and wide steps up to double doors.  |
| `counting-house`       | 2×2       | A dignified ledger office with narrow tall windows, a green copper roof, and an enclosed courtyard wall.       |
| `treasury-vault`       | 1×1       | A small squat stone vault inside a courtyard — thick walls, heavy iron-banded door. Goes inside Counting House.|
| `library-row-building` | 1×1       | A single narrow timber-and-brick library — tall shelves visible through a window, small chimney.               |
| `observatory`          | 2×2       | A domed stone observatory with a slit in the dome revealing a brass telescope.                                 |
| `scribes-office`       | 1×1       | A small plastered office with a slate roof, a quill-and-ink signpost outside, one window full of loose papers. |
| `post-office`          | 2×2       | A timber mail hall with a loading bay, small wheeled mail carts parked outside, a flag post (no flag drawn).   |
| `bazaar`               | 2×2       | Open-air market stalls arranged in a ring, canvas awnings, wooden crates stacked between stalls.               |
| `cartographers-guild`  | 1×1       | A compact stone office with a large rolled-map signpost and a weather vane shaped like a compass rose.         |

### The Keep (cold slate / iron / banked ember)

| Slug          | Footprint | Description                                                                                                               |
|---------------|-----------|---------------------------------------------------------------------------------------------------------------------------|
| `the-smelter` | 3×3       | An industrial furnace keep: dark slate walls, a single tall iron smokestack, glowing ember vents at the base.             |
| `quarry`      | 2×2       | An excavated stone pit at ground level with neatly stacked blocks of cut stone around its rim; a wooden crane over it.    |
| `jail`        | 2×2       | A grim stone fortress with small barred windows evenly spaced along its walls and a reinforced iron door.                 |
| `scouts-hut`  | 1×1       | A tiny lookout hut with a thin antenna spike on its roof, perched on a small rise. Small enough to tuck into a Jail cell. |

### Off-map caravan markers (not buildings)

Generate these once at 512×512. These decorate the map edges to represent external
providers.

| Slug                | Description                                                                           |
|---------------------|---------------------------------------------------------------------------------------|
| `caravan-gold`      | A covered wagon laden with gold-trimmed chests, a single draft animal in harness.     |
| `caravan-mail`      | A light courier cart piled with sealed letter sacks and a small bell on a hook.       |
| `raven-cartographer`| A single raven in flight carrying a tiny rolled scroll in its beak.                   |
| `foundation-stamp`  | A stone foundation plate with a subtle geometric seal, rendered flat-to-ground.       |

---

## Stage 3 — Villager and Customer Tier Figures

Small figures populating the village. Generate each at 256×256, transparent, isometric.
Attach `style-sheet.png`.

| Slug                  | Description                                                                                                         |
|-----------------------|---------------------------------------------------------------------------------------------------------------------|
| `villager-idle`       | A generic standing villager silhouette with a simple earth-toned tunic. No face features.                           |
| `villager-walking`    | The same villager, mid-stride walk cycle frame.                                                                     |
| `tent-small`          | A single conical canvas tent with a low smokeless fire ring outside.                                                |
| `tent-cluster`        | A cluster of three canvas tents sharing a small gathering area, footprint 2×2.                                      |
| `house-cottage`       | A modest timber-and-plaster cottage, 1×1, thatched roof, small chimney smoking gently.                              |
| `manor-placeholder`   | A generic enterprise manor, 2×2, two-story stone with a crest plinth out front (crest left blank for per-tenant).   |

---

## Stage 4 — State Overlay Elements (reusable, not per-building)

Generate these once; the runtime composites them over building sprites as cheaper than
regenerating every building × every state. Transparent, 256×256.

| Slug                 | Description                                                                                               |
|----------------------|-----------------------------------------------------------------------------------------------------------|
| `overlay-warning`    | A soft amber pennant on a thin pole, hangs downward with a gentle curl.                                   |
| `overlay-critical`   | A cluster of small flame tongues and wisps of dark smoke, anchored to a single point.                     |
| `overlay-info`       | A calm blue pennant, same shape as the warning pennant.                                                   |
| `overlay-upgrading`  | A small scaffolding lattice of pale timber with a single hanging rope pulley.                             |

---

## Stage 5 — Export & Naming

Export each sprite as PNG with transparency.

```
village-sprites/
  style-sheet.png
  gate-district/
    the-gate.healthy.png
    the-gate.degraded.png
    the-gate.critical.png
    the-gate.offline.png
    ...
  town-proper/
    ...
  the-keep/
    ...
  caravans/
    ...
  figures/
    ...
  overlays/
    ...
```

File naming: `<slug>.<state>.png`. State omitted for caravans, figures, and overlays.

---

## Stage 6 — Iteration Protocol

1. Generate the full Gate District at Healthy state first. Stop.
2. Review: do Healthy variants read as a coherent set? If not, adjust the style sheet and
   regenerate — not the individual buildings.
3. Only once a district's Healthy variants are coherent, generate its Degraded, Critical,
   and Offline variants.
4. Move to the next district.

This ordering is deliberate. It is far cheaper to throw away and re-seed the style sheet
once than to regenerate 100+ sprites that drifted apart in subtle palette or lighting.

---

## Known Nano Banana limitations to plan around

- Face features drift; the brief explicitly forbids faces on villagers for this reason.
- Small text on signage is unreliable; signs are blank in this brief and text is overlaid
  at render time.
- Subtle tier-palette differences (Gate / Town / Keep) require reinforcement — always
  attach the style sheet, always name the palette in the prompt.
- Transparent backgrounds are sometimes imperfect; plan for a one-pass background cleanup
  in an editor before shipping.
