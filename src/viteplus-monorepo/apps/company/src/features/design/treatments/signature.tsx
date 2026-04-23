import type { CSSProperties, ReactNode } from "react";
import { Lockup, WingsChip, type LockupVariant } from "@forge-metal/brand";

// The Guardian email signature, three voices.
//
// Every outgoing Guardian email carries a signature from one of three
// treatments. Each variant is a miniature of its treatment:
//
//   workshop — Argent on Iron (the production console). No Fraunces. Geist
//              Mono throughout — name, role, contact. Amber LIVE dot marker
//              is the treatment's signature status glyph. Wings-only mark
//              (no wordmark, matching the live console chrome).
//   newsroom — Ink-on-Flare acid broadcast. No rule — the ground carries
//              the signpost. Emboss-variant mark (black circular medallion)
//              because Argent-on-Flare contrast is thin for the wings.
//   letters  — Ink-on-Paper editorial. Fraunces sets the author's name;
//              Geist handles role / contact. Bordeaux vertical rule left
//              of the identity mirrors the pull-quote treatment from the
//              article body.

export type SignatureVariant = "workshop" | "newsroom" | "letters";

export type SignatureMarkVariant = LockupVariant | "wings-only";

export type SignatureAccent = {
  readonly hex: string;
  readonly style: "hairline" | "dot" | "rule-left" | "none";
  readonly heightPx?: number;
  readonly label?: string;
};

type VariantTokens = {
  readonly background: string;
  readonly border: string;
  readonly textColor: string;
  readonly mutedStrong: string;
  readonly mutedDefault: string;
  readonly eyebrowColor: string;
  readonly nameFont: string;
  readonly nameSizePx: number;
  readonly nameWeight: number;
  readonly nameFontVariationSettings?: string;
  readonly roleFont: string;
  readonly contactFont: string;
  readonly defaultMarkColor: string;
};

// Treatment-scoped typographic + surface tokens. Kept co-located with the
// signature (rather than pulled into @forge-metal/brand) while the API
// stabilises; promote to brand once a second consumer shows up.
const VARIANT_TOKENS: Record<SignatureVariant, VariantTokens> = {
  workshop: {
    // Iron ground mirrors the live console chrome. The signature reads as
    // a terminal row rather than a business-card; everything sets in Geist
    // Mono so the operator's last touchpoint to Guardian inherits the
    // tenant-console vocabulary.
    background: "#0e0e0e",
    border: "1px solid rgba(245,245,245,0.10)",
    textColor: "var(--color-type-iron)",
    mutedStrong: "rgba(245,245,245,0.82)",
    mutedDefault: "rgba(245,245,245,0.60)",
    eyebrowColor: "var(--treatment-muted-faint)",
    nameFont: "'Geist Mono', ui-monospace, SFMono-Regular, monospace",
    nameSizePx: 14,
    nameWeight: 600,
    nameFontVariationSettings: '"wght" 600',
    roleFont: "'Geist Mono', ui-monospace, SFMono-Regular, monospace",
    contactFont: "'Geist Mono', ui-monospace, SFMono-Regular, monospace",
    defaultMarkColor: "var(--color-type-iron)",
  },
  newsroom: {
    // Flare ground is the signpost. No rule needed — the acid green IS the
    // accent. Black emboss mark keeps the wings legible over Flare (where
    // Argent's 1.05:1 contrast would fail WCAG).
    background: "var(--color-flare)",
    border: "1px solid rgba(11,11,11,0.22)",
    textColor: "var(--color-ink)",
    mutedStrong: "rgba(11,11,11,0.82)",
    mutedDefault: "rgba(11,11,11,0.70)",
    eyebrowColor: "var(--treatment-muted-faint)",
    nameFont: "'Geist', sans-serif",
    nameSizePx: 15,
    nameWeight: 600,
    roleFont: "'Geist', sans-serif",
    contactFont: "'Geist', sans-serif",
    defaultMarkColor: "var(--color-ink)",
  },
  letters: {
    // Paper ground with Fraunces for the author name. The Bordeaux rule
    // moves to the card's left edge (`rule-left` accent style below), which
    // mirrors the pull-quote rule in the article body above. Reading the
    // signature after reading the pull-quote should feel like the same
    // editorial grammar re-applied.
    background: "var(--color-paper)",
    border: "1px solid rgba(11,11,11,0.14)",
    textColor: "var(--color-ink)",
    mutedStrong: "rgba(11,11,11,0.78)",
    mutedDefault: "rgba(11,11,11,0.60)",
    eyebrowColor: "var(--treatment-muted-faint)",
    nameFont: "'Fraunces', Georgia, serif",
    nameSizePx: 20,
    nameWeight: 400,
    nameFontVariationSettings: '"opsz" 72, "SOFT" 50',
    roleFont: "'Geist', sans-serif",
    contactFont: "'Geist', sans-serif",
    defaultMarkColor: "var(--color-ink)",
  },
};

export type TreatmentSignatureProps = {
  readonly variant: SignatureVariant;
  readonly eyebrow: ReactNode;
  readonly markVariant: SignatureMarkVariant;
  readonly markColor?: string;
  readonly markAside?: ReactNode;
  readonly identity: { readonly name: string; readonly role: string };
  readonly accent: SignatureAccent;
  readonly meta?: ReactNode;
  readonly contact: { readonly email: string; readonly secondary?: string };
};

export function TreatmentSignature(props: TreatmentSignatureProps) {
  const { variant, eyebrow, markVariant, identity, accent, meta, contact } = props;
  const t = VARIANT_TOKENS[variant];

  const cardStyle: CSSProperties = {
    background: t.background,
    color: t.textColor,
    padding: "22px 24px",
    borderRadius: "8px",
    fontFamily: t.contactFont,
    fontSize: "13px",
    maxWidth: "540px",
    border: t.border,
    // The Letters variant lives its Bordeaux rule on the card's left edge
    // (the same gesture the pull-quote makes in the article body). Other
    // variants leave this slot untouched.
    ...(accent.style === "rule-left"
      ? {
          borderLeft: `${accent.heightPx ?? 3}px solid ${accent.hex}`,
          paddingLeft: `${Math.max(18, 24 - (accent.heightPx ?? 3))}px`,
        }
      : {}),
  };

  return (
    <div>
      <div
        style={{
          font: '600 10px/1 "Geist Mono", ui-monospace, monospace',
          fontVariationSettings: '"wght" 600',
          letterSpacing: "0.16em",
          textTransform: "uppercase",
          color: t.eyebrowColor,
          marginBottom: "10px",
        }}
      >
        {eyebrow}
      </div>
      <div style={cardStyle}>
        <SignatureMarkRow
          variant={markVariant}
          tokens={t}
          {...(props.markColor ? { color: props.markColor } : {})}
          {...(props.markAside ? { aside: props.markAside } : {})}
        />
        <div
          style={{
            fontFamily: t.nameFont,
            fontWeight: t.nameWeight,
            fontSize: `${t.nameSizePx}px`,
            fontVariationSettings: t.nameFontVariationSettings,
            color: t.textColor,
            lineHeight: 1.15,
          }}
        >
          {identity.name}
        </div>
        <div
          style={{
            fontFamily: t.roleFont,
            color: t.mutedDefault,
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
              color: t.mutedDefault,
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
            color: t.mutedDefault,
            fontSize: "12px",
            fontFamily: t.contactFont,
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

function SignatureMarkRow({
  variant,
  tokens,
  color,
  aside,
}: {
  readonly variant: SignatureMarkVariant;
  readonly tokens: VariantTokens;
  readonly color?: string;
  readonly aside?: ReactNode;
}) {
  if (variant === "wings-only") {
    return (
      <div style={{ display: "flex", alignItems: "center", gap: "10px", marginBottom: "14px" }}>
        {/* Wings carried inside their own dark chip so they remain Argent
            regardless of the card's ground colour. */}
        <WingsChip style={{ width: "22px", height: "22px", flex: "0 0 22px" }} />
        {aside ? <SignatureAside color={tokens.mutedDefault}>{aside}</SignatureAside> : null}
      </div>
    );
  }
  return (
    <div style={{ display: "flex", alignItems: "center", gap: "12px", marginBottom: "14px" }}>
      <Lockup size="sm" variant={variant} wordmarkColor={color ?? tokens.defaultMarkColor} />
      {aside ? <SignatureAside color={tokens.mutedDefault}>{aside}</SignatureAside> : null}
    </div>
  );
}

function SignatureAside({
  color,
  children,
}: {
  readonly color: string;
  readonly children: ReactNode;
}) {
  return (
    <span
      style={{
        fontFamily: "'Geist Mono', ui-monospace, monospace",
        fontSize: "11px",
        fontWeight: 600,
        fontVariationSettings: '"wght" 600',
        letterSpacing: "0.14em",
        textTransform: "uppercase",
        color,
      }}
    >
      {children}
    </span>
  );
}

// Status badge helper — Amber dot plus uppercase mono label. Used by the
// Workshop signature meta slot as the treatment's "live / pageable" glyph.
// Exported for treatments to compose in via the `meta` prop.
export function SignatureStatusBadge({
  accentHex,
  children,
  onDark,
}: {
  readonly accentHex: string;
  readonly children: ReactNode;
  readonly onDark?: boolean;
}) {
  return (
    <span
      style={{
        display: "inline-flex",
        alignItems: "center",
        gap: "8px",
        fontSize: "11px",
        fontFamily: "'Geist Mono', ui-monospace, monospace",
        letterSpacing: "0.12em",
        textTransform: "uppercase",
        color: onDark ? "rgba(245,245,245,0.72)" : "rgba(11,11,11,0.6)",
      }}
    >
      <span
        aria-hidden="true"
        style={{
          width: "8px",
          height: "8px",
          borderRadius: "50%",
          background: accentHex,
          boxShadow: `0 0 0 2px ${accentHex}38`,
        }}
      />
      {children}
    </span>
  );
}

function SignatureAccentMarker({ accent }: { readonly accent: SignatureAccent }) {
  if (accent.style === "none" || accent.style === "rule-left") return null;
  if (accent.style === "dot") {
    return (
      <div
        style={{
          display: "inline-flex",
          alignItems: "center",
          gap: "8px",
          fontSize: "11px",
          fontFamily: "'Geist Mono', ui-monospace, monospace",
          fontWeight: 600,
          fontVariationSettings: '"wght" 600',
          letterSpacing: "0.18em",
          textTransform: "uppercase",
          color: "rgba(11,11,11,0.72)",
        }}
      >
        <span
          aria-hidden="true"
          style={{
            width: "9px",
            height: "9px",
            borderRadius: "50%",
            background: accent.hex,
            boxShadow: `0 0 0 2px ${accent.hex}44`,
          }}
        />
        {accent.label ? <span>{accent.label}</span> : null}
      </div>
    );
  }
  // hairline
  return (
    <div
      aria-hidden="true"
      style={{
        height: `${accent.heightPx ?? 2}px`,
        width: "44px",
        background: accent.hex,
        margin: "4px 0 0",
        borderRadius: "1px",
      }}
    />
  );
}
