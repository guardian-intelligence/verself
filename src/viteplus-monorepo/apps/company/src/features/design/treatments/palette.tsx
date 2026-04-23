import type { CSSProperties, ReactNode } from "react";

// Fixed columnar palette grammar. Every treatment declares the same four roles
// in the same order — Ground, Accent, Mark, Muted — so a reader parsing
// multiple treatments on the same page sees a parallel cadence. A treatment
// that genuinely has no muted role (e.g. Newsroom is broadcast — no body type
// lives there) passes `role: undefined` for muted and the column renders a
// quiet placeholder rather than disappearing; the grammar is the teaching
// device, absence has to be visible.

export type PaletteRoleId = "ground" | "accent" | "mark" | "muted";

const ROLE_ORDER: readonly PaletteRoleId[] = ["ground", "accent", "mark", "muted"];

const ROLE_LABEL: Record<PaletteRoleId, string> = {
  ground: "Ground",
  accent: "Accent",
  mark: "Mark",
  muted: "Muted",
};

export type PaletteSwatch = {
  readonly name: string;
  readonly hex: string;
  readonly pantone?: string;
  readonly note?: string;
  readonly chipStyle?: CSSProperties;
};

// The palette is keyed by role, not by index. Treatments pass an object with
// whichever roles they fill; a missing role renders the "not used" cell.
export type TreatmentPaletteRoles = Partial<Record<PaletteRoleId, PaletteSwatch>>;

const PANEL_BG = "#111113";
const LINE = "#2a2a2f";

export function TreatmentPalette({
  roles,
  rule,
}: {
  readonly roles: TreatmentPaletteRoles;
  readonly rule?: ReactNode;
}) {
  return (
    <div
      data-treatment="company"
      style={{
        marginBottom: "16px",
        padding: "18px 20px",
        border: `1px solid ${LINE}`,
        borderRadius: "10px",
        background: PANEL_BG,
        color: "var(--treatment-ink)",
      }}
    >
      <div className="treatment-palette-grid">
        {ROLE_ORDER.map((role) => {
          const swatch = roles[role];
          return (
            <div key={role} className="treatment-palette-cell">
              <div className="treatment-palette-role">{ROLE_LABEL[role]}</div>
              {swatch ? (
                <PaletteFilled swatch={swatch} />
              ) : (
                <PaletteEmpty roleLabel={ROLE_LABEL[role]} />
              )}
            </div>
          );
        })}
      </div>
      {rule ? (
        <div
          style={{
            marginTop: "14px",
            paddingTop: "14px",
            borderTop: `1px solid ${LINE}`,
            fontFamily: "'Geist', sans-serif",
            fontSize: "12px",
            color: "var(--treatment-muted)",
            lineHeight: 1.5,
          }}
        >
          {rule}
        </div>
      ) : null}
      <style>{PALETTE_CSS}</style>
    </div>
  );
}

function PaletteFilled({ swatch }: { readonly swatch: PaletteSwatch }) {
  return (
    <div style={{ display: "flex", alignItems: "center", gap: "12px", minWidth: 0 }}>
      <div
        aria-hidden="true"
        style={{
          width: "40px",
          height: "40px",
          borderRadius: "6px",
          flex: "0 0 40px",
          background: swatch.hex,
          // Inset 1px hairline so dark chips separate from the dark panel and
          // light chips (Paper, Argent) get a visible edge. Neutral rgba works
          // for both ends of the ramp.
          boxShadow: "inset 0 0 0 1px rgba(128,128,128,0.25)",
          ...swatch.chipStyle,
        }}
      />
      <div style={{ minWidth: 0 }}>
        <div
          style={{
            fontFamily: "'Geist', sans-serif",
            fontWeight: 600,
            fontSize: "13px",
            color: "var(--color-type-iron)",
            lineHeight: 1.2,
          }}
        >
          {swatch.name}
        </div>
        {/* Hex and pantone set on independent lines so "PANTONE 715 C" never
           spills across the cell boundary. The pantone slot is reserved even
           when absent so every palette cell across every treatment shares the
           same three-line rhythm (name / code / note) — removes a class of
           grid-column-width regressions when a new pantone gets added. */}
        <div
          style={{
            font: '600 10px/1.35 "Geist Mono", ui-monospace, monospace',
            fontVariationSettings: '"wght" 600',
            color: "var(--treatment-muted-faint)",
            letterSpacing: "0.08em",
            textTransform: "uppercase",
            marginTop: "2px",
          }}
        >
          {swatch.hex}
        </div>
        <div
          style={{
            font: '500 10px/1.35 "Geist Mono", ui-monospace, monospace',
            fontVariationSettings: '"wght" 500',
            color: "var(--treatment-muted-faint)",
            letterSpacing: "0.08em",
            textTransform: "uppercase",
            marginTop: "1px",
            minHeight: "13px",
          }}
        >
          {swatch.pantone ?? " "}
        </div>
        {swatch.note ? (
          <div
            style={{
              fontFamily: "'Geist', sans-serif",
              fontSize: "11px",
              color: "var(--treatment-muted)",
              marginTop: "4px",
              lineHeight: 1.35,
            }}
          >
            {swatch.note}
          </div>
        ) : null}
      </div>
    </div>
  );
}

function PaletteEmpty({ roleLabel }: { readonly roleLabel: string }) {
  // Matches PaletteFilled's three-line rhythm: name ("not used"), code
  // (reserved empty slot the pantone line occupies in filled cells), note
  // ("This treatment declines <role>.") — so an absent role doesn't push the
  // grid row taller than adjacent filled cells.
  return (
    <div style={{ display: "flex", alignItems: "center", gap: "12px", minWidth: 0 }}>
      <div
        aria-hidden="true"
        style={{
          width: "40px",
          height: "40px",
          borderRadius: "6px",
          flex: "0 0 40px",
          background:
            "repeating-linear-gradient(135deg, rgba(245,245,245,0.04) 0 6px, transparent 6px 12px)",
          border: "1px dashed rgba(245,245,245,0.18)",
        }}
      />
      <div style={{ minWidth: 0 }}>
        <div
          style={{
            fontFamily: "'Geist Mono', ui-monospace, monospace",
            fontSize: "10px",
            fontWeight: 600,
            fontVariationSettings: '"wght" 600',
            color: "var(--treatment-muted-faint)",
            letterSpacing: "0.12em",
            textTransform: "uppercase",
            lineHeight: 1.2,
          }}
        >
          not used
        </div>
        <div
          style={{
            font: '500 10px/1.35 "Geist Mono", ui-monospace, monospace',
            fontVariationSettings: '"wght" 500',
            color: "var(--treatment-muted-faint)",
            letterSpacing: "0.08em",
            textTransform: "uppercase",
            marginTop: "1px",
            minHeight: "13px",
          }}
        >
          {" "}
        </div>
        <div
          style={{
            fontFamily: "'Geist', sans-serif",
            fontSize: "11px",
            color: "var(--treatment-muted)",
            marginTop: "4px",
            lineHeight: 1.35,
          }}
        >
          This treatment declines {roleLabel.toLowerCase()}.
        </div>
      </div>
    </div>
  );
}

// Four columns at md+, two at small. Keeping this in a style block (rather
// than Tailwind utility classes) so the component has no external class-name
// dependency when we extract it into @forge-metal/brand later.
const PALETTE_CSS = `
  .treatment-palette-grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 18px 24px;
  }
  @media (min-width: 768px) {
    .treatment-palette-grid {
      grid-template-columns: repeat(4, minmax(0, 1fr));
    }
  }
  .treatment-palette-cell { display: flex; flex-direction: column; gap: 8px; min-width: 0; }
  .treatment-palette-role {
    font: 600 10px/1 "Geist Mono", ui-monospace, monospace;
    font-variation-settings: "wght" 600;
    letter-spacing: 0.16em;
    text-transform: uppercase;
    color: var(--treatment-muted-faint);
  }
`;
