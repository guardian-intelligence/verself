import type { CSSProperties, ReactNode } from "react";
import { Lockup, WingsArgent, type LockupVariant } from "@forge-metal/brand";

// One signature skeleton. All four treatments render:
//
//   ┌────────────────────────────────────────────┐
//   │ [mark row]         — per-treatment variant │
//   │ [identity row]     Name  ·  Role           │
//   │ [accent row]       hairline or dot         │
//   │ [meta row]         optional slot           │
//   │ [contact row]      email  ·  secondary     │
//   └────────────────────────────────────────────┘
//
// Per-treatment knobs:
//   • markVariant    — "argent" | "chip" | "emboss" | "wings-only"
//   • accent         — { hex, style: "hairline" | "dot" | "none" }
//   • identity       — { name, role } — never a valediction. Letters moved
//                      its "— the founder" line into the article body.
//   • meta           — ReactNode slot for per-treatment extras (Workshop's
//                      amber status dot, Letters' "Filed from Seattle · № 3").
//   • paperGround    — flip the card to Paper (Letters only); otherwise white.

export type SignatureMarkVariant = LockupVariant | "wings-only";

export type SignatureAccent = {
  readonly hex: string;
  readonly style: "hairline" | "dot" | "none";
};

export type TreatmentSignatureProps = {
  readonly eyebrow: ReactNode;
  readonly markVariant: SignatureMarkVariant;
  readonly markColor?: string;
  readonly identity: { readonly name: string; readonly role: string };
  readonly accent: SignatureAccent;
  readonly meta?: ReactNode;
  readonly contact: { readonly email: string; readonly secondary?: string };
  readonly paperGround?: boolean;
};

const LINE_DARK = "#2a2a2f";

export function TreatmentSignature(props: TreatmentSignatureProps) {
  const { eyebrow, markVariant, identity, accent, meta, contact, paperGround } = props;

  const cardStyle: CSSProperties = {
    background: paperGround ? "var(--color-paper)" : "#fff",
    color: "var(--color-ink)",
    padding: "20px 22px",
    borderRadius: "8px",
    fontFamily: "'Geist', sans-serif",
    fontSize: "13px",
    maxWidth: "540px",
    border: "1px solid rgba(11,11,11,0.12)",
  };

  return (
    <div>
      <div
        style={{
          font: '600 10px/1 "Geist Mono", ui-monospace, monospace',
          fontVariationSettings: '"wght" 600',
          letterSpacing: "0.16em",
          textTransform: "uppercase",
          color: "var(--muted-faint)",
          marginBottom: "10px",
        }}
      >
        {eyebrow}
      </div>
      <div style={cardStyle}>
        <SignatureMarkRow
          variant={markVariant}
          {...(props.markColor ? { color: props.markColor } : {})}
        />
        <div
          style={{
            fontFamily: "'Geist', sans-serif",
            fontWeight: 600,
            fontSize: "15px",
            color: "var(--color-ink)",
          }}
        >
          {identity.name}
        </div>
        <div
          style={{
            color: "rgba(11,11,11,0.65)",
            fontSize: "13px",
            marginTop: "2px",
            marginBottom: "12px",
          }}
        >
          {identity.role}
        </div>
        <SignatureAccentMarker accent={accent} />
        {meta ? (
          <div
            style={{
              marginTop: "10px",
              marginBottom: "4px",
              fontSize: "12px",
              color: "rgba(11,11,11,0.65)",
            }}
          >
            {meta}
          </div>
        ) : null}
        <div
          style={{
            marginTop: "12px",
            display: "flex",
            gap: "12px",
            color: "rgba(11,11,11,0.65)",
            fontSize: "12px",
            flexWrap: "wrap",
          }}
        >
          <span>{contact.email}</span>
          {contact.secondary ? (
            <>
              <span aria-hidden="true">·</span>
              <span>{contact.secondary}</span>
            </>
          ) : null}
        </div>
      </div>
    </div>
  );
}

function SignatureMarkRow({ variant, color }: { readonly variant: SignatureMarkVariant; readonly color?: string }) {
  if (variant === "wings-only") {
    // Workshop-style: no wordmark. Wings persist at 22 px as the identity
    // anchor (matches the live console chrome).
    return (
      <div style={{ display: "flex", alignItems: "center", gap: "10px", marginBottom: "14px" }}>
        <WingsArgent style={{ width: "22px", height: "22px", flex: "0 0 22px" }} />
      </div>
    );
  }
  return (
    <div style={{ marginBottom: "14px" }}>
      <Lockup size="sm" variant={variant} wordmarkColor={color ?? "var(--color-ink)"} />
    </div>
  );
}

function SignatureAccentMarker({ accent }: { readonly accent: SignatureAccent }) {
  if (accent.style === "none") return null;
  if (accent.style === "dot") {
    return (
      <div
        style={{
          display: "inline-flex",
          alignItems: "center",
          gap: "8px",
          fontSize: "11px",
          fontFamily: "'Geist Mono', ui-monospace, monospace",
          letterSpacing: "0.12em",
          textTransform: "uppercase",
          color: "rgba(11,11,11,0.65)",
        }}
      >
        <span
          aria-hidden="true"
          style={{
            width: "8px",
            height: "8px",
            borderRadius: "50%",
            background: accent.hex,
            boxShadow: `0 0 0 2px ${accent.hex}33`,
          }}
        />
      </div>
    );
  }
  // hairline
  return (
    <div
      aria-hidden="true"
      style={{
        height: "2px",
        width: "44px",
        background: accent.hex,
        margin: "4px 0 0",
        borderRadius: "1px",
      }}
    />
  );
}

// Export to let e2e tests query by data-testid without having to walk the
// whole DOM. Unused for now; we'll wire it up when the first spec lands.
export const SIGNATURE_DARK_LINE = LINE_DARK;
