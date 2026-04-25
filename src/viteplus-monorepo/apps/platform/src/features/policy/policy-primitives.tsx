import { Link as LinkIcon } from "lucide-react";
import type { ReactNode } from "react";

import { cn } from "@verself/ui/lib/utils";

import {
  deriveMailboxes,
  effectiveDateOf,
  formatPrettyDate,
  latestVersion,
  type Mailboxes,
  type PolicyId,
} from "~/lib/policy-catalog";

// Shared rendering primitives for every /policy/* page. Keeping each page's
// layout routed through one set of components means that a tone or structural
// change (adding a revision banner, say) lands in one place instead of nine.

export function PolicyArticle({ children }: { children: ReactNode }) {
  return (
    <article className="flex flex-col gap-10 [&_h2]:scroll-mt-[var(--header-scroll-offset)] [&_h3]:scroll-mt-[var(--header-scroll-offset)]">
      {children}
    </article>
  );
}

// Policy documents carry an Effective date derived from the changelog entry
// that last listed them. Meta-documents (the changelog itself) carry only a
// Last-updated timestamp; omit `policyId` in that case.
export function PolicyHeader({
  eyebrow,
  title,
  policyId,
}: {
  eyebrow?: string;
  title: string;
  policyId?: PolicyId;
}) {
  const lastUpdated = formatPrettyDate(latestVersion().date);
  return (
    <header className="flex flex-col gap-4 border-b border-border pb-8">
      <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
        {eyebrow ?? "Platform Policy"}
      </p>
      <h1 className="text-3xl font-semibold tracking-tight md:text-4xl">{title}</h1>
      <dl className="flex flex-wrap gap-x-8 gap-y-2 text-sm">
        {policyId ? (
          <MetaPair label="Effective date" value={formatPrettyDate(effectiveDateOf(policyId))} />
        ) : null}
        <MetaPair label="Last updated" value={lastUpdated} />
      </dl>
    </header>
  );
}

function MetaPair({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-baseline gap-2">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="font-medium tabular-nums">{value}</dd>
    </div>
  );
}

export function SectionHeading({ id, children }: { id: string; children: ReactNode }) {
  return (
    <h2 id={id} className="group flex items-baseline gap-2 text-2xl font-semibold tracking-tight">
      <span>{children}</span>
      <a
        href={`#${id}`}
        aria-label="Anchor"
        className="opacity-0 transition-opacity group-hover:opacity-60"
      >
        <LinkIcon className="size-4" />
      </a>
    </h2>
  );
}

export function SubHeading({ id, children }: { id: string; children: ReactNode }) {
  return (
    <h3 id={id} className="text-lg font-semibold tracking-tight">
      {children}
    </h3>
  );
}

export function Prose({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div
      className={cn(
        "flex flex-col gap-4 text-sm leading-6 text-foreground/90 md:text-[15px] md:leading-7",
        "[&_p]:max-w-[68ch] [&_ul]:max-w-[68ch] [&_ol]:max-w-[68ch]",
        "[&_ul]:flex [&_ul]:flex-col [&_ul]:gap-2 [&_ul]:pl-5 [&_ul]:list-disc",
        "[&_ol]:flex [&_ol]:flex-col [&_ol]:gap-2 [&_ol]:pl-5 [&_ol]:list-decimal",
        "[&_a]:underline [&_a]:decoration-muted-foreground/60 [&_a]:underline-offset-4 [&_a:hover]:decoration-foreground",
        "[&_code]:rounded [&_code]:bg-secondary [&_code]:px-1.5 [&_code]:py-0.5 [&_code]:text-[0.85em]",
        "[&_strong]:font-semibold [&_strong]:text-foreground",
        className,
      )}
    >
      {children}
    </div>
  );
}

export function SummaryPanel({ children }: { children: ReactNode }) {
  return (
    <aside className="relative rounded-lg border border-border bg-secondary/50 p-5 pl-6 before:absolute before:inset-y-3 before:left-0 before:w-0.5 before:rounded-full before:bg-foreground">
      <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
        At a glance
      </p>
      <div className="mt-3 grid gap-x-6 gap-y-2 text-sm leading-6 md:grid-cols-2">{children}</div>
    </aside>
  );
}

export function SummaryItem({ term, children }: { term: string; children: ReactNode }) {
  return (
    <div>
      <strong className="font-medium">{term}:</strong> {children}
    </div>
  );
}

export function DefinitionGrid({ children }: { children: ReactNode }) {
  return <dl className="grid gap-4 md:grid-cols-2">{children}</dl>;
}

export function DefinitionCard({ term, definition }: { term: string; definition: ReactNode }) {
  return (
    <div className="flex flex-col gap-1.5 rounded-lg border border-border bg-card p-4">
      <dt className="font-medium">{term}</dt>
      <dd className="text-sm leading-6 text-muted-foreground">{definition}</dd>
    </div>
  );
}

export function ContactSection({
  id = "contact",
  operatorDomain,
  primary = "policy",
}: {
  id?: string;
  operatorDomain: string;
  primary?: keyof Mailboxes;
}) {
  const m = deriveMailboxes(operatorDomain);
  const primaryAddress = m[primary];
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id={id}>Contact</SectionHeading>
      <Prose>
        <p>
          Questions and requests under this policy go to{" "}
          <a href={`mailto:${primaryAddress}`}>{primaryAddress}</a>. Security reports go to{" "}
          <a href={`mailto:${m.security}`}>{m.security}</a> and take precedence over routine policy
          correspondence. GDPR data-protection correspondence may be directed to{" "}
          <a href={`mailto:${m.dpo}`}>{m.dpo}</a>; abuse reports to{" "}
          <a href={`mailto:${m.abuse}`}>{m.abuse}</a>.
        </p>
      </Prose>
    </section>
  );
}

export function ChangesSection({ policyId }: { policyId: PolicyId }) {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="changes">Changes to this policy</SectionHeading>
      <Prose>
        <p>
          Material changes take effect 30 days after they are announced by email to the
          administrators on each affected organization. The effective date at the top of this page
          is the date the current version took effect. Prior versions of all policies live at{" "}
          <a href="/policy/changelog">/policy/changelog</a>, and every change is recorded there in
          commit-addressable form.
        </p>
        <p>
          <em>Policy identifier:</em> <code>{policyId}</code>.
        </p>
      </Prose>
    </section>
  );
}
