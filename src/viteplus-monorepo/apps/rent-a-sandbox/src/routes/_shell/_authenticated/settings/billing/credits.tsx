import { createFileRoute, Link } from "@tanstack/react-router";
import { Button } from "@forge-metal/ui/components/ui/button";
import { Card, CardContent } from "@forge-metal/ui/components/ui/card";
import {
  PageEyebrow,
  PageSection,
  PageSections,
  SectionDescription,
  SectionHeader,
  SectionHeaderContent,
  SectionTitle,
} from "@forge-metal/ui/components/ui/page";
import { ErrorCallout } from "~/components/error-callout";
import { useCreateCheckoutSessionMutation } from "~/features/billing/mutations";

export const Route = createFileRoute("/_shell/_authenticated/settings/billing/credits")({
  component: CreditsPage,
});

const CREDIT_PACKS = [
  { cents: 1000, label: "$10" },
  { cents: 2500, label: "$25" },
  { cents: 5000, label: "$50" },
  { cents: 10000, label: "$100" },
];

function CreditsPage() {
  const mutation = useCreateCheckoutSessionMutation();

  return (
    <PageSections>
      <PageSection>
        <PageEyebrow>
          <Link to="/settings/billing" className="hover:text-foreground">
            ← Back to billing
          </Link>
        </PageEyebrow>
        <SectionHeader>
          <SectionHeaderContent>
            <SectionTitle>Purchase credits</SectionTitle>
            <SectionDescription>
              Add prepaid account balance for usage beyond your current bucket allowances.
            </SectionDescription>
          </SectionHeaderContent>
        </SectionHeader>

        <div className="grid gap-4 md:grid-cols-4">
          {CREDIT_PACKS.map((pack) => (
            <Card key={pack.cents} className="transition-colors hover:bg-accent/30">
              <CardContent className="flex flex-col items-center gap-2 py-6">
                <span className="font-mono text-2xl font-semibold tabular-nums">{pack.label}</span>
                <span className="text-xs text-muted-foreground">Account top-up</span>
                <Button
                  type="button"
                  className="mt-2 w-full"
                  onClick={() => mutation.mutate(pack.cents)}
                  disabled={mutation.isPending}
                >
                  Buy
                </Button>
              </CardContent>
            </Card>
          ))}
        </div>

        {mutation.error ? <ErrorCallout error={mutation.error} title="Checkout failed" /> : null}
      </PageSection>
    </PageSections>
  );
}
