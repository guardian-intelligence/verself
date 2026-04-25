import { WingsChip } from "@verself/brand";
import { RulesRow, Section } from "../section-shell";
import { Colophon } from "../colophon";
import { LINE, sectionMeta, Surface } from "../shared";
import {
  Nameplate,
  TreatmentMarkCard,
  TreatmentMastheadLadder,
  TreatmentPalette,
  TreatmentSignature,
  TreatmentTypeLadder,
} from "../treatments";

// ============================================================================
// 06 — Treatments · Letters
// ============================================================================
export function SectionLetters() {
  const meta = sectionMeta("letters");
  return (
    <Section
      meta={meta}
      lede={
        <>
          <i>Letters</i> is Guardian's long-form surface, where individual voices show their work.
          Paper ground, tracked-Geist GUARDIAN · LETTERS masthead, Fraunces body for flowing prose,
          Geist for bylines and metadata. The page optimises for legibility on warm stone-paper: Ink
          sets the body, the drop-cap initial, the nameplate rule, and every CTA.{" "}
          <b style={{ color: "var(--color-bordeaux)" }}>Bordeaux</b> is reserved for one thing only
          — the blockquote left-rule, where an editor has chosen to slow the reader and let another
          voice speak. Flare does not appear on Letters; it is too loud for reading.
        </>
      }
    >
      <TreatmentPalette
        roles={{
          ground: {
            name: "Paper",
            hex: "#F6F4ED",
            note: "Long-form reading canvas · warm stone-paper.",
            chipStyle: { boxShadow: `inset 0 0 0 1px ${LINE}` },
          },
          accent: {
            name: "Bordeaux",
            hex: "#5C1F1E",
            pantone: "Pantone 504 C",
            note: "Blockquote left-rule — the ONLY place Bordeaux ships.",
          },
          mark: {
            name: "Argent",
            hex: "#FFFFFF",
            note: "Wings, always — inside the iron chip on paper.",
            // Argent-on-Paper would otherwise lose its edge (1.05:1 contrast);
            // a 1 px Stone hairline makes the swatch read as a card on the
            // Paper ground so the reader can locate it.
            chipStyle: { boxShadow: "inset 0 0 0 1px rgba(11,11,11,0.18)" },
          },
          muted: {
            name: "Ink",
            hex: "#0B0B0B",
            note: "Body, drop cap, nameplate, CTA; meta at 0.7 / 0.6 / 0.55 (Stone).",
            chipStyle: { background: "rgba(11,11,11,0.7)" },
          },
        }}
        rule={
          <>
            Bordeaux never ships outside the blockquote rule. CTAs, drop caps, nameplate rules, tab
            underlines, and active link underlines all paint Ink — the editorial register gets its
            voice from Fraunces and from Paper's warmth, not from ornament. The muted register on
            Paper is Ink at 0.7 / 0.6 / 0.55 opacity (Stone), tuned to hold WCAG AA on small text
            against #F6F4ED.
          </>
        }
      />

      {/* Mark specimen + Type ladder — Letters' "rules" pair. The Paper
          carrier with iron chip sits left; the Fraunces-heavy type ladder
          sits right. The masthead ladder rides full width below — the
          720 px nameplate specimen needs the horizontal room. */}
      <RulesRow>
        <TreatmentMarkCard
          groundVar="var(--color-paper)"
          rows={[
            { label: "Argent · Paper · chip", value: "Letters", emphasise: "name" },
            { label: "ground", value: "#F6F4ED", emphasise: "hex" },
            { label: "chip", value: "#0E0E0E", emphasise: "hex" },
            { label: "wings", value: "#FFFFFF", emphasise: "hex" },
          ]}
        >
          <WingsChip style={{ width: "58%", height: "auto" }} />
        </TreatmentMarkCard>
        {/* Letters is the only treatment where Fraunces sets body prose;
            Geist only handles bylines and metadata. */}
        <TreatmentTypeLadder
          rows={[
            {
              sample: "Applied intelligence is not an adjective.",
              role: "article · h1",
              spec: "Fraunces / 64 / 1.02 / -25 · opsz 144 · SOFT 50",
              sampleSizePx: 48,
              sampleStyle: {
                fontFamily: "'Fraunces', Georgia, serif",
                fontVariationSettings: '"opsz" 144, "SOFT" 50',
                fontWeight: 400,
                fontSize: "48px",
                lineHeight: 1.02,
                letterSpacing: "-0.025em",
              },
            },
            {
              sample:
                "There is a tradition in the software industry of taking a good word and pointing it at something that has not yet earned it.",
              role: "body prose · fraunces",
              spec: "Fraunces / 19 / 1.55 · opsz 18 · Regular",
              sampleSizePx: 19,
              sampleStyle: {
                fontFamily: "'Fraunces', Georgia, serif",
                fontVariationSettings: '"opsz" 18, "SOFT" 0',
                fontWeight: 400,
                fontSize: "19px",
                lineHeight: 1.55,
              },
            },
            {
              sample: "— the founder",
              role: "valediction · italic",
              spec: "Fraunces / 22 / italic · opsz 72 · SOFT 60",
              sampleSizePx: 22,
              sampleStyle: {
                fontFamily: "'Fraunces', Georgia, serif",
                fontVariationSettings: '"opsz" 72, "SOFT" 60',
                fontStyle: "italic",
                fontWeight: 400,
                fontSize: "22px",
                lineHeight: 1.3,
                color: "var(--treatment-muted-strong)",
              },
            },
            {
              sample: "By the founder · Filed from Seattle, WA",
              role: "byline · meta",
              spec: "Geist / 13 / 1.5 · Stone 0.7",
              sampleSizePx: 13,
              sampleStyle: {
                fontFamily: "'Geist', sans-serif",
                fontWeight: 400,
                fontSize: "13px",
                lineHeight: 1.5,
                color: "var(--treatment-muted)",
              },
            },
            {
              sample: "Letters № 3 · 19 Apr 2026 · 8 min read",
              role: "eyebrow · upper",
              spec: "Geist / 11 / 1 / +240 · 500 · UPPER",
              sampleSizePx: 11,
              sampleStyle: {
                fontFamily: "'Geist', sans-serif",
                fontWeight: 500,
                fontSize: "11px",
                lineHeight: 1,
                letterSpacing: "0.24em",
                textTransform: "uppercase",
                color: "var(--treatment-muted)",
              },
            },
          ]}
          caption={
            <>
              Letters is the only treatment where Fraunces sets body prose. If a surface wants to
              use Fraunces for body outside Letters, it probably wants to be a Letter.
            </>
          }
        />
      </RulesRow>
      <div style={{ marginBottom: "16px" }}>
        {/* Masthead ladder at full width — the 720 px nameplate specimen
            needs the horizontal room to demonstrate the ceiling variant. */}
        <TreatmentMastheadLadder
          rows={[
            { widthPx: 720, issue: "№ 3", date: "19 Apr 2026", label: "ceiling" },
            { widthPx: 480, issue: "№ 3", date: "19 Apr 2026", label: "proportional" },
            { widthPx: 320, issue: "№ 3", date: "19 Apr 2026", label: "floor" },
          ]}
          footer={
            <>
              The nameplate is a volume masthead, not a wordmark lockup. Wings + tracked uppercase
              Geist + 1 px Ink rule. The article H1 below it does the heading work; the nameplate
              identifies the publication, not the article.
            </>
          }
        />
      </div>

      <Surface
        ground="paper"
        className="shell-paper"
        style={{ padding: "clamp(32px, 5vw, 64px) clamp(20px, 4vw, 56px)", borderRadius: "16px" }}
      >
        {/* Thin-ruled nameplate — replaces the old size="md" chip Lockup that
            was visually competing with the article H1 below. Wings + tracked
            uppercase Geist "Guardian · Letters" + 1 px Ink rule. */}
        <div style={{ margin: "0 0 28px" }}>
          <Nameplate />
        </div>
        {/* Article metadata whispers above the headline. Earlier iterations
            set this at 12 px / 500 with a 24 px flex gap between three spans —
            without explicit separators the three tokens read as one long
            clump ("№ 3  19 APRIL 2026  8 MIN READ"), and the size competed
            with the H1 below for visual weight. 11 px Geist / 0.16 em
            tracking / Stone 0.55 pushes the row below the H1's perceptual
            threshold so the reader's eye starts at the headline, not the
            meta. Middot separators make the tripartite reading semantically
            legible. */}
        <div
          style={{
            fontFamily: "'Geist', sans-serif",
            fontSize: "11px",
            fontWeight: 500,
            letterSpacing: "0.16em",
            textTransform: "uppercase",
            color: "rgba(11,11,11,0.55)",
            margin: "0 0 18px",
            display: "flex",
            gap: "10px",
            alignItems: "center",
          }}
        >
          <span>№&nbsp;3</span>
          <span aria-hidden="true">·</span>
          <span>19 April 2026</span>
          <span aria-hidden="true">·</span>
          <span>8 min read</span>
        </div>
        <h1
          style={{
            fontFamily: "'Fraunces', Georgia, serif",
            fontVariationSettings: '"opsz" 144, "SOFT" 50, "WONK" 0',
            fontWeight: 400,
            fontSize: "clamp(36px, 6vw, 64px)",
            lineHeight: 1.02,
            letterSpacing: "-0.025em",
            margin: "0 0 20px",
            color: "var(--color-ink)",
            maxWidth: "18ch",
            textTransform: "none",
          }}
        >
          Applied intelligence is not an adjective.
        </h1>
        <div
          style={{
            fontFamily: "'Geist', sans-serif",
            fontSize: "13px",
            color: "#5d5a52",
            margin: "0 0 36px",
            display: "flex",
            gap: "16px",
            alignItems: "center",
          }}
        >
          <span>By the founder</span>
          <span
            style={{ width: "4px", height: "4px", background: "#5d5a52", borderRadius: "2px" }}
          />
          <span>Filed from Seattle, WA</span>
        </div>
        <p
          style={{
            fontFamily: "'Fraunces', Georgia, serif",
            fontVariationSettings: '"opsz" 18, "SOFT" 0',
            fontWeight: 400,
            fontSize: "19px",
            lineHeight: 1.55,
            color: "var(--color-ink)",
            maxWidth: "58ch",
            margin: "0 0 20px",
          }}
        >
          <span
            aria-hidden="true"
            style={{
              fontFamily: "'Fraunces', Georgia, serif",
              fontVariationSettings: '"opsz" 144, "SOFT" 50',
              fontWeight: 400,
              fontSize: "clamp(56px, 8vw, 88px)",
              lineHeight: 0.9,
              float: "left",
              margin: "6px 14px 0 0",
              // Drop cap paints Ink — the initial letter is the article's
              // flare (flourish), set in the display weight of the body face.
              // Bordeaux is reserved for the blockquote left-rule only.
              color: "var(--color-ink)",
            }}
          >
            T
          </span>
          here is a tradition in the software industry of taking a good word and pointing it at
          something that has not yet earned it. &lsquo;Intelligent&rsquo; dishwashers.{" "}
          &lsquo;Smart&rsquo; calendars. &lsquo;AI-powered&rsquo; spreadsheets. Guardian
          Intelligence is not a linguistic claim. It is a specification. An applied intelligence
          firm ships workloads that run, bills that settle, and companies that scale past their
          founder without hiring a second person.
        </p>
        <blockquote
          style={{
            fontFamily: "'Fraunces', Georgia, serif",
            fontStyle: "italic",
            fontVariationSettings: '"opsz" 72, "SOFT" 60',
            fontWeight: 400,
            fontSize: "clamp(22px, 3.4vw, 34px)",
            lineHeight: 1.2,
            letterSpacing: "-0.012em",
            margin: "32px 0",
            padding: "0 0 0 20px",
            borderLeft: "3px solid var(--color-bordeaux)",
            color: "var(--color-ink)",
            maxWidth: "40ch",
          }}
        >
          &ldquo;A 10,000× increase in value-generation per capita is not a slogan. It is an
          engineering target.&rdquo;
        </blockquote>
        {/* Valediction — this is where the italic "— the founder" belongs,
            inside the article body above the sign-off, not where the author's
            name should be in the signature card. Moved from the old
            LettersSignature. */}
        <p
          style={{
            fontFamily: "'Fraunces', Georgia, serif",
            fontVariationSettings: '"opsz" 72, "SOFT" 60',
            fontStyle: "italic",
            fontWeight: 400,
            fontSize: "clamp(20px, 2.6vw, 26px)",
            lineHeight: 1.3,
            letterSpacing: "-0.01em",
            margin: "40px 0 0",
            color: "var(--color-ink)",
          }}
        >
          — the founder
        </p>
      </Surface>
      <div style={{ marginTop: "24px" }}>
        {/* The Letters signature drops the "Filed from Seattle, WA · Letter № 3"
            meta row. Both facts already appear in the article body above it. */}
        <TreatmentSignature
          variant="letters"
          eyebrow="Email signature · Letters"
          markVariant="chip"
          identity={{
            name: "Founder Name",
            role: "Founder · Guardian Intelligence",
          }}
          accent={{ hex: "var(--color-ink)", style: "rule-left", heightPx: 3 }}
          contact={{
            email: "letters@guardianintelligence.org",
            secondary: "guardianintelligence.org/letters",
          }}
        />
      </div>

      {/* CTA specimen — the rule in living form. Bordeaux has retreated to
          the blockquote left-rule ONLY; CTAs, drop caps, nameplate rules,
          and link underlines all paint Ink. A Bordeaux button would read as
          a pull-quote that escaped the article body; a Bordeaux drop cap
          would signal "accent" where the page already has one. */}
      <div
        style={{
          padding: "clamp(28px, 4vw, 48px) clamp(20px, 4vw, 48px)",
          border: "1px solid rgba(11,11,11,0.14)",
          borderRadius: "12px",
          background: "var(--color-paper)",
          color: "var(--color-ink)",
          marginTop: "16px",
          marginBottom: "16px",
        }}
      >
        <p
          className="font-mono text-[11px] font-semibold uppercase tracking-[0.18em]"
          style={{
            color: "rgba(11,11,11,0.55)",
            fontVariationSettings: '"wght" 600',
            margin: "0 0 14px",
          }}
        >
          Controls · Letters
        </p>
        <h3
          style={{
            fontFamily: "'Fraunces', Georgia, serif",
            fontVariationSettings: '"opsz" 144, "SOFT" 30',
            fontWeight: 400,
            fontSize: "clamp(28px, 3.6vw, 40px)",
            lineHeight: 1.05,
            letterSpacing: "-0.022em",
            color: "var(--color-ink)",
            margin: "0 0 20px",
            maxWidth: "24ch",
          }}
        >
          Ink for controls. Ink for the drop cap.
        </h3>
        <p
          style={{
            fontFamily: "'Geist', sans-serif",
            fontSize: "15px",
            lineHeight: 1.55,
            color: "rgba(11,11,11,0.7)",
            margin: "0 0 24px",
            maxWidth: "56ch",
          }}
        >
          Bordeaux is reserved for the blockquote left-rule. Everything else that wants to carry
          editorial weight — the initial drop cap, the nameplate hairline, the primary CTA, the
          active link underline — paints Ink. A Letters page earns its voice from Fraunces on warm
          Paper, not from wine-coloured ornament scattered across every accent slot.
        </p>
        <span
          className="rounded-md"
          style={{
            display: "inline-flex",
            alignItems: "center",
            gap: "10px",
            fontFamily: "'Geist', sans-serif",
            fontWeight: 500,
            fontSize: "14px",
            padding: "10px 18px",
            background: "var(--color-ink)",
            color: "var(--color-paper)",
            border: "1px solid var(--color-ink)",
          }}
        >
          Read the letter
          <span aria-hidden="true">→</span>
        </span>
      </div>

      <div style={{ marginTop: "24px" }}>
        <Colophon
          heading="Letters · Specifications"
          rows={[
            { label: "Ground", value: "#F6F4ED", note: "Paper · warm stone-paper canvas" },
            {
              label: "Body type",
              value: "#0B0B0B",
              note: "Ink on Paper — optimised for legibility",
            },
            {
              label: "Blockquote rule",
              value: "#5C1F1E",
              note: "Pantone 504 C · Bordeaux · the ONLY Bordeaux on the page",
            },
            { label: "CTA", value: "Ink button · Paper text", note: "Never Bordeaux" },
            {
              label: "Drop cap",
              value: "Fraunces 88 px · Ink",
              note: "Display opsz, initial letter of the article",
            },
            { label: "Nameplate rule", value: "Ink 1 px", note: "Under GUARDIAN LETTERS masthead" },
            { label: "Mark", value: "WingsChip", note: "Argent wings in iron chip" },
            {
              label: "Masthead",
              value: "Geist 11 / +260 / UPPER",
              note: "Tracked uppercase volume line",
            },
            { label: "Body face", value: "Fraunces", note: "opsz 18 · SOFT 0 · 19 / 1.55" },
            { label: "Byline", value: "Geist 13", note: "Stone at 0.7 opacity" },
            { label: "Button radius", value: "rounded-md", note: "0.375rem · shadcn base" },
          ]}
          footer={
            <>
              Letters is the only treatment where Fraunces sets body prose. Bordeaux ships in
              exactly one place — the blockquote left-rule — because a Letter reads as a page of Ink
              on warm Paper with a single wine-coloured interruption where another voice breaks in.
              Flare and Amber do not appear here.
            </>
          }
        />
      </div>
    </Section>
  );
}
