import { WingsArgent } from "@forge-metal/brand";
import { RulesRow, Section } from "../section-shell";
import { LINE, sectionMeta } from "../shared";
import {
  SignatureStatusBadge,
  TreatmentMarkCard,
  TreatmentPalette,
  TreatmentSignature,
  TreatmentTypeLadder,
  TreatmentWingsOnlyLadder,
} from "../treatments";

// ============================================================================
// 04 — Treatments · Workshop
//
// Workshop is the productivity treatment. Two load-bearing rules:
//   1. No Fraunces. Everything sets in Geist + Geist Mono.
//   2. No Guardian wordmark in the chrome. The operator's customer is
//      thinking about their tenant, not about Guardian. Wings persist (22 px)
//      as a quiet identity anchor but never lock up with text.
// Amber is the workshop accent; Flare is banned.
// ============================================================================
export function SectionWorkshop() {
  const meta = sectionMeta("workshop");
  return (
    <Section
      meta={meta}
      lede={
        <>
          Inside the product, the chrome disappears. Everything — navigation, controls, data, code —
          sets in Geist and Geist Mono; Fraunces is absent. The chrome carries the tenant's name,
          not ours — a founder working here is thinking about their own company, not about Guardian.
          Wings persist at 22 px as a quiet identity anchor. The one accent is{" "}
          <b style={{ color: "var(--color-amber)" }}>Amber</b>: primary actions, live states,
          positive signals. Flare is banned from workshop surfaces.
        </>
      }
    >
      <TreatmentPalette
        roles={{
          ground: { name: "Iron", hex: "#0E0E0E", note: "The workshop floor." },
          accent: {
            name: "Amber",
            hex: "#F79326",
            pantone: "Pantone 715 C",
            note: "Primary actions, live state, positive signals.",
          },
          mark: { name: "Argent", hex: "#FFFFFF", note: "Wings only · 22 px in chrome." },
          muted: {
            name: "Ash",
            hex: "#F5F5F5",
            note: "Body & meta · Geist + Geist Mono.",
            chipStyle: { background: "rgba(245,245,245,0.72)" },
          },
        }}
        rule={
          <>
            Amber reads as <i>work is happening here</i> — a nod to Bloomberg Terminal's amber
            phosphor. <b>Flare is banned from Workshop</b> and{" "}
            <b>Amber never ships outside Workshop</b>; the two accents trade places at the chrome
            boundary so an operator always knows which context they are inside.
          </>
        }
      />

      {/* Mark specimen + Type ladder — Workshop's "rules" pair. Mark carrier
          left (wings only — no wordmark ever), type ladder right. The
          wings-only size ladder stays full-width below so it can show the
          full 64 → 16 descent without cramping. */}
      <RulesRow>
        <TreatmentMarkCard
          groundVar="var(--color-iron)"
          rows={[
            { label: "Argent · Iron · wings only", value: "Workshop", emphasise: "name" },
            { label: "ground", value: "#0E0E0E", emphasise: "hex" },
            { label: "wings", value: "#FFFFFF", emphasise: "hex" },
          ]}
        >
          <WingsArgent style={{ width: "56%", height: "auto" }} cropped />
        </TreatmentMarkCard>
        <TreatmentTypeLadder
          rows={[
            {
              sample: "Sandbox execution",
              role: "h3 · ui · workshop",
              spec: "Geist / 20 / 1.3 / -10 · SemiBold",
              sampleSizePx: 20,
              sampleStyle: {
                fontFamily: "'Geist', sans-serif",
                fontWeight: 600,
                fontSize: "20px",
                lineHeight: 1.3,
                letterSpacing: "-0.01em",
              },
            },
            {
              sample:
                "14 active across 4 tenants · 3 h 22 m median lease · 99.98% attestation rate.",
              role: "body",
              spec: "Geist / 14 / 1.5 · Regular",
              sampleSizePx: 14,
              sampleStyle: {
                fontFamily: "'Geist', sans-serif",
                fontWeight: 400,
                fontSize: "14px",
                lineHeight: 1.5,
              },
            },
            {
              sample: "tenant · region · workload · status",
              role: "small · ash",
              spec: "Geist / 12 / 1.5 · Regular · Ash",
              sampleSizePx: 12,
              sampleStyle: {
                fontFamily: "'Geist', sans-serif",
                fontWeight: 400,
                fontSize: "12px",
                lineHeight: 1.5,
                color: "var(--treatment-muted)",
              },
            },
            {
              sample: "0x41e9f2a  attest=true  lease=3h22m  region=us-east-1",
              role: "mono",
              spec: "Geist Mono / 12 / 1.5 · Regular",
              sampleSizePx: 12,
              sampleStyle: {
                fontFamily: "'Geist Mono', ui-monospace, monospace",
                fontWeight: 400,
                fontSize: "12px",
                lineHeight: 1.5,
                color: "var(--treatment-muted)",
              },
            },
            {
              sample: "LIVE · PAGEABLE · US-EAST-1",
              role: "badge · pageable",
              spec: "Geist Mono / 10 / 1 / +180 · 600 · UPPER",
              sampleSizePx: 10,
              sampleStyle: {
                fontFamily: "'Geist Mono', ui-monospace, monospace",
                fontWeight: 600,
                fontVariationSettings: '"wght" 600',
                fontSize: "10px",
                lineHeight: 1,
                letterSpacing: "0.18em",
                textTransform: "uppercase",
                color: "var(--color-amber)",
              },
            },
          ]}
          caption={
            <>
              Workshop declines Fraunces entirely. If an editor ever asks for a serif inside the
              product, that's a smell: the surface probably belongs under Letters or Newsroom, not
              Workshop.
            </>
          }
        />
      </RulesRow>
      <div style={{ marginBottom: "16px" }}>
        <TreatmentWingsOnlyLadder
          note={
            <>
              22 px is the size the live console chrome ships. Below 22 px the glyph starts to lose
              its lower-wing tip at typical display DPI; above 64 px the wings feel like a logo
              looking for a sentence.
            </>
          }
        />
      </div>
      <div
        style={{
          background: "var(--color-iron)",
          color: "var(--color-type-iron)",
          borderRadius: "12px",
          overflow: "hidden",
          border: `1px solid ${LINE}`,
        }}
      >
        <div
          style={{
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            padding: "14px 20px",
            borderBottom: `1px solid ${LINE}`,
            flexWrap: "wrap",
            gap: "12px",
          }}
        >
          {/* Header identity: wings + tenant name in Geist. No Fraunces,
              no "Guardian" wordmark. */}
          <div style={{ display: "flex", alignItems: "center", gap: "10px" }}>
            <WingsArgent style={{ width: "22px", height: "22px" }} />
            <span
              style={{
                fontFamily: "'Geist', sans-serif",
                fontWeight: 500,
                fontSize: "14px",
                letterSpacing: "-0.005em",
              }}
            >
              acme-corp
            </span>
            <span
              style={{
                fontFamily: "'Geist Mono', ui-monospace, monospace",
                fontSize: "11px",
                fontWeight: 600,
                fontVariationSettings: '"wght" 600',
                color: "var(--treatment-muted-faint)",
                letterSpacing: "0.08em",
              }}
            >
              / production
            </span>
          </div>
          <nav
            style={{
              display: "flex",
              gap: "24px",
              fontFamily: "'Geist', sans-serif",
              fontSize: "13px",
            }}
          >
            <span style={{ color: "var(--color-type-iron)" }}>Overview</span>
            <span style={{ color: "var(--treatment-muted)" }}>Compute</span>
            <span style={{ color: "var(--treatment-muted)" }}>Integrations</span>
            <span style={{ color: "var(--treatment-muted)" }}>Leases</span>
            <span style={{ color: "var(--treatment-muted)" }}>Billing</span>
          </nav>
          <div style={{ display: "flex", gap: "10px", alignItems: "center" }}>
            <span
              style={{
                display: "inline-flex",
                alignItems: "center",
                gap: "6px",
                fontSize: "10px",
                fontWeight: 600,
                fontVariationSettings: '"wght" 600',
                letterSpacing: "0.16em",
                textTransform: "uppercase",
                padding: "4px 10px",
                borderRadius: "999px",
                border: "1px solid rgba(245,245,245,0.2)",
                fontFamily: "'Geist Mono', ui-monospace, monospace",
                color: "rgba(245,245,245,0.72)",
              }}
            >
              <span
                aria-hidden="true"
                style={{
                  width: "6px",
                  height: "6px",
                  borderRadius: "50%",
                  background: "var(--color-amber)",
                  boxShadow: "0 0 0 2px rgba(247,147,38,0.22)",
                }}
              />
              Live
            </span>
            <button
              style={{
                fontFamily: "'Geist', sans-serif",
                fontWeight: 500,
                fontSize: "13px",
                padding: "8px 14px",
                borderRadius: "6px",
                border: "1px solid var(--color-amber)",
                background: "var(--color-amber)",
                color: "var(--color-ink)",
                cursor: "pointer",
              }}
            >
              Deploy
            </button>
          </div>
        </div>
        <div className="workshop-body">
          <aside className="workshop-aside">
            <style>{`
              .workshop-body {
                display: grid;
                grid-template-columns: 1fr;
                min-height: 420px;
              }
              .workshop-aside {
                border-bottom: 1px solid ${LINE};
                padding: 16px 20px;
                font-family: 'Geist', sans-serif;
                font-size: 13px;
              }
              @media (min-width: 768px) {
                .workshop-body { grid-template-columns: 220px 1fr; }
                .workshop-aside {
                  border-right: 1px solid ${LINE};
                  border-bottom: 0;
                  padding: 20px 16px;
                }
              }
            `}</style>
            <div
              style={{
                color: "var(--treatment-muted-faint)",
                fontSize: "10px",
                fontWeight: 600,
                fontVariationSettings: '"wght" 600',
                fontFamily: "'Geist Mono', ui-monospace, monospace",
                letterSpacing: "0.16em",
                textTransform: "uppercase",
                margin: "0 8px 8px",
              }}
            >
              Workspace
            </div>
            {[
              { label: "Overview", active: true },
              { label: "Sandboxes" },
              { label: "Leases" },
              { label: "Attestations" },
            ].map((item) => (
              <span
                key={item.label}
                style={{
                  display: "block",
                  padding: "8px 10px",
                  borderRadius: "6px",
                  color: item.active ? "var(--color-type-iron)" : "var(--treatment-muted)",
                  background: item.active ? "#1c1c20" : "transparent",
                }}
              >
                {item.label}
              </span>
            ))}
            <div
              style={{
                color: "var(--treatment-muted-faint)",
                fontSize: "10px",
                fontWeight: 600,
                fontVariationSettings: '"wght" 600',
                fontFamily: "'Geist Mono', ui-monospace, monospace",
                letterSpacing: "0.16em",
                textTransform: "uppercase",
                margin: "20px 8px 8px",
              }}
            >
              Account
            </div>
            {["Integrations", "Billing", "Settings"].map((label) => (
              <span
                key={label}
                style={{
                  display: "block",
                  padding: "8px 10px",
                  borderRadius: "6px",
                  color: "var(--treatment-muted)",
                }}
              >
                {label}
              </span>
            ))}
          </aside>
          <div style={{ padding: "clamp(20px, 3vw, 28px) clamp(20px, 3vw, 32px)", minWidth: 0 }}>
            <h2
              style={{
                fontFamily: "'Geist', sans-serif",
                fontWeight: 600,
                fontSize: "clamp(20px, 2.4vw, 24px)",
                lineHeight: 1.2,
                letterSpacing: "-0.01em",
                margin: "0 0 6px",
                color: "var(--color-type-iron)",
                textTransform: "none",
              }}
            >
              Production sandboxes
            </h2>
            <p
              style={{
                color: "var(--treatment-muted)",
                fontFamily: "'Geist', sans-serif",
                fontSize: "14px",
                margin: "0 0 20px",
              }}
            >
              14 active across 4 tenants · 3 h 22 m median lease · 99.98% attestation rate
            </p>
            <div style={{ overflowX: "auto" }}>
              <table
                style={{
                  width: "100%",
                  borderCollapse: "collapse",
                  fontFamily: "'Geist', sans-serif",
                  fontSize: "13px",
                  minWidth: "520px",
                }}
              >
                <thead>
                  <tr>
                    {["Tenant", "Region", "Workload", "Lease", "Status"].map((col, i) => (
                      <th
                        key={col}
                        style={{
                          padding: "12px 14px",
                          textAlign: i === 3 ? "right" : "left",
                          borderBottom: `1px solid ${LINE}`,
                          fontSize: "10px",
                          fontFamily: "'Geist Mono', ui-monospace, monospace",
                          letterSpacing: "0.14em",
                          textTransform: "uppercase",
                          color: "var(--treatment-muted-faint)",
                          fontWeight: 600,
                          fontVariationSettings: '"wght" 600',
                        }}
                      >
                        {col}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {[
                    [
                      "acme-corp",
                      "us-east-1",
                      "inference · h100×8",
                      "0x41e9f2a",
                      "● attested",
                      "ok",
                    ],
                    ["hex-labs", "us-east-1", "ci · runner-pool", "0x41e9f2b", "● attested", "ok"],
                    [
                      "lumen-mail",
                      "eu-west-1",
                      "stateful · zfs-pool",
                      "0x41e9f2c",
                      "○ draining",
                      "warn",
                    ],
                    [
                      "solo-founder",
                      "us-west-2",
                      "editor · agent-vm",
                      "0x41e9f2d",
                      "● attested",
                      "ok",
                    ],
                  ].map((row) => (
                    <tr key={row[0]}>
                      <td style={{ padding: "12px 14px", borderBottom: `1px solid ${LINE}` }}>
                        {row[0]}
                      </td>
                      <td style={{ padding: "12px 14px", borderBottom: `1px solid ${LINE}` }}>
                        {row[1]}
                      </td>
                      <td style={{ padding: "12px 14px", borderBottom: `1px solid ${LINE}` }}>
                        {row[2]}
                      </td>
                      <td
                        style={{
                          padding: "12px 14px",
                          borderBottom: `1px solid ${LINE}`,
                          fontFamily: "'Geist Mono', ui-monospace, monospace",
                          fontSize: "12px",
                          color: "var(--color-type-iron)",
                          textAlign: "right",
                        }}
                      >
                        {row[3]}
                      </td>
                      <td
                        style={{
                          padding: "12px 14px",
                          borderBottom: `1px solid ${LINE}`,
                          color: row[5] === "ok" ? "var(--color-amber)" : "#f0c74f",
                          fontWeight: 500,
                        }}
                      >
                        {row[4]}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <pre
              style={{
                background: "#050505",
                color: "#d4d4d4",
                padding: "16px 18px",
                margin: "20px 0 0",
                borderRadius: "8px",
                font: '12px/1.6 "Geist Mono", ui-monospace, monospace',
                overflow: "auto",
                border: `1px solid ${LINE}`,
              }}
            >
              <span style={{ color: "#5d5a52", fontStyle: "italic" }}>
                {"// Deploy a sandbox from the Metal CLI."}
              </span>
              {"\n"}
              <span style={{ color: "#C0C0F2" }}>import</span>
              {" { sandbox } "}
              <span style={{ color: "#C0C0F2" }}>from</span>{" "}
              <span style={{ color: "var(--color-amber)" }}>{`"@metal/compute"`}</span>;{"\n\n"}
              <span style={{ color: "#C0C0F2" }}>await</span> sandbox.run({"{"}
              {"\n"}
              {"  tenant:   "}
              <span style={{ color: "var(--color-amber)" }}>{`"acme-corp"`}</span>,{"\n"}
              {"  image:    "}
              <span style={{ color: "var(--color-amber)" }}>{`"ubuntu-24.04"`}</span>,{"\n"}
              {"  accel:    "}
              <span style={{ color: "var(--color-amber)" }}>{`"h100x8"`}</span>,{"\n"}
              {"  attest:   "}
              <span style={{ color: "#C0C0F2" }}>true</span>,{"\n"}
              {"});"}
            </pre>
          </div>
        </div>
      </div>
      <div style={{ marginTop: "24px" }}>
        <TreatmentSignature
          variant="workshop"
          eyebrow="Email signature · Workshop"
          markVariant="wings-only"
          markAside="Platform · Engineering"
          identity={{
            name: "Engineer Name",
            role: "Platform Engineering · On-call, us-east-1",
          }}
          accent={{ hex: "#F79326", style: "none" }}
          meta={
            <SignatureStatusBadge accentHex="#F79326" onDark>
              incident response · pageable
            </SignatureStatusBadge>
          }
          contact={{ email: "engineer@guardianintelligence.org" }}
        />
      </div>
    </Section>
  );
}
