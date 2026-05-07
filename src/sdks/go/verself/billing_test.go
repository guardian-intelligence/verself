package verself

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBillingUsesPublicAPI(t *testing.T) {
	var checkoutKey string
	var contractKey string
	var changeKey string
	var portalKey string
	var cancelKey string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer tok_billing" {
			t.Fatalf("%s %s Authorization = %q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/entitlements":
			_, _ = w.Write([]byte(`{"org_id":"370200542594579812","universal":{"scope_type":"account","product_id":"","product_display":"","bucket_id":"","bucket_display":"","sku_id":"","sku_display":"","coverage_label":"Account","available_units":"100","pending_units":"0","period_start_units":"100","spent_units":"0","sources":[]},"products":[]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/grants":
			if r.URL.Query().Get("product_id") != "sandbox" || r.URL.Query().Get("active") != "true" {
				t.Fatalf("grants query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"grants":[{"grant_id":"grant_1","scope_type":"product","scope_product_id":"sandbox","scope_bucket_id":"","scope_sku_id":"","source":"promo","source_reference_id":"manual","entitlement_period_id":"","policy_version":"v1","starts_at":"2026-05-06T00:00:00Z","available":"100","pending":"0"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/billing-documents":
			if r.URL.Query().Get("product_id") != "sandbox" {
				t.Fatalf("documents query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"documents":[{"document_id":"doc_1","document_number":"INV-1","document_kind":"statement","finalization_id":"fin_1","product_id":"sandbox","cycle_id":"cycle_1","status":"issued","payment_status":"not_due","period_start":"2026-05-01T00:00:00Z","period_end":"2026-06-01T00:00:00Z","currency":"USD","subtotal_units":"0","adjustment_units":"0","tax_units":"0","total_due_units":"0"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/contracts":
			_, _ = w.Write([]byte(`{"contracts":[{"contract_id":"contract_1","product_id":"sandbox","plan_id":"sandbox-pro","cadence_kind":"monthly","status":"active","payment_state":"current","entitlement_state":"active","phase_id":"phase_1","starts_at":"2026-05-06T00:00:00Z"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/plans":
			if r.URL.Query().Get("product_id") != "sandbox" {
				t.Fatalf("plans query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"plans":[{"plan_id":"sandbox-pro","product_id":"sandbox","display_name":"Sandbox Pro","tier":"pro","billing_mode":"subscription","currency":"USD","monthly_amount_cents":"9900","annual_amount_cents":"99000","active":true,"is_default":true}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/statement":
			if r.URL.Query().Get("product_id") != "sandbox" {
				t.Fatalf("statement query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"org_id":"370200542594579812","product_id":"sandbox","period_source":"current","period_start":"2026-05-01T00:00:00Z","period_end":"2026-06-01T00:00:00Z","generated_at":"2026-05-06T00:00:00Z","currency":"USD","unit_label":"credits","totals":{"reserved_units":"0","contract_units":"100","free_tier_units":"0","promo_units":"0","purchase_units":"0","receivable_units":"0","refund_units":"0","charge_units":"5","total_due_units":"0"},"grant_summaries":[],"line_items":[]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/checkout":
			checkoutKey = r.Header.Get("Idempotency-Key")
			_, _ = w.Write([]byte(`{"url":"https://billing.example.test/checkout"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/contracts":
			contractKey = r.Header.Get("Idempotency-Key")
			_, _ = w.Write([]byte(`{"url":"https://billing.example.test/contract"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/contracts/contract_1/changes":
			changeKey = r.Header.Get("Idempotency-Key")
			_, _ = w.Write([]byte(`{"url":"https://billing.example.test/change","change_id":"change_1","finalization_id":"","document_id":"","status":"checkout_required","price_delta_units":"5000"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/portal":
			portalKey = r.Header.Get("Idempotency-Key")
			_, _ = w.Write([]byte(`{"url":"https://billing.example.test/portal"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/contracts/contract_1/cancel":
			cancelKey = r.Header.Get("Idempotency-Key")
			_, _ = w.Write([]byte(`{"contract":{"contract_id":"contract_1","product_id":"sandbox","plan_id":"sandbox-pro","cadence_kind":"monthly","status":"canceling","payment_state":"current","entitlement_state":"active","phase_id":"phase_1","starts_at":"2026-05-06T00:00:00Z"}}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client, err := New(Options{BearerToken: "tok_billing", BillingURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	entitlements, err := client.Billing.GetEntitlements(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if entitlements.Universal.AvailableUnits != "100" {
		t.Fatalf("unexpected entitlements: %#v", entitlements)
	}
	grants, err := client.Billing.ListGrants(context.Background(), BillingListGrantsOptions{ProductID: "sandbox", Active: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(grants.Grants) != 1 || grants.Grants[0].GrantID != "grant_1" {
		t.Fatalf("unexpected grants: %#v", grants)
	}
	documents, err := client.Billing.ListDocuments(context.Background(), BillingListDocumentsOptions{ProductID: "sandbox"})
	if err != nil {
		t.Fatal(err)
	}
	if len(documents.Documents) != 1 || documents.Documents[0].DocumentID != "doc_1" {
		t.Fatalf("unexpected documents: %#v", documents)
	}
	contracts, err := client.Billing.ListContracts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(contracts.Contracts) != 1 || contracts.Contracts[0].ContractID != "contract_1" {
		t.Fatalf("unexpected contracts: %#v", contracts)
	}
	plans, err := client.Billing.ListPlans(context.Background(), BillingListPlansOptions{ProductID: "sandbox"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plans.Plans) != 1 || plans.Plans[0].PlanID != "sandbox-pro" {
		t.Fatalf("unexpected plans: %#v", plans)
	}
	statement, err := client.Billing.GetStatement(context.Background(), BillingStatementOptions{ProductID: "sandbox"})
	if err != nil {
		t.Fatal(err)
	}
	if statement.Totals.TotalDueUnits != "0" {
		t.Fatalf("unexpected statement: %#v", statement)
	}
	checkout, err := client.Billing.CreateCheckoutSession(context.Background(), BillingCheckoutInput{ProductID: "sandbox", AmountCents: 1000, SuccessURL: "https://console.example.test/success", CancelURL: "https://console.example.test/cancel", IdempotencyKey: "billing:checkout"})
	if err != nil || checkout.URL == "" {
		t.Fatalf("unexpected checkout url=%#v err=%v", checkout, err)
	}
	contract, err := client.Billing.CreateContractSession(context.Background(), BillingContractInput{PlanID: "sandbox-pro", Cadence: "monthly", SuccessURL: "https://console.example.test/success", CancelURL: "https://console.example.test/cancel", IdempotencyKey: "billing:contract"})
	if err != nil || contract.URL == "" {
		t.Fatalf("unexpected contract url=%#v err=%v", contract, err)
	}
	change, err := client.Billing.CreateContractChangeSession(context.Background(), BillingContractChangeInput{ContractID: "contract_1", TargetPlanID: "sandbox-enterprise", SuccessURL: "https://console.example.test/success", CancelURL: "https://console.example.test/cancel", IdempotencyKey: "billing:change"})
	if err != nil || change.ChangeID != "change_1" {
		t.Fatalf("unexpected change=%#v err=%v", change, err)
	}
	portal, err := client.Billing.CreatePortalSession(context.Background(), BillingPortalInput{ReturnURL: "https://console.example.test/billing", IdempotencyKey: "billing:portal"})
	if err != nil || portal.URL == "" {
		t.Fatalf("unexpected portal url=%#v err=%v", portal, err)
	}
	canceled, err := client.Billing.CancelContract(context.Background(), BillingCancelContractInput{ContractID: "contract_1", IdempotencyKey: "billing:cancel"})
	if err != nil || canceled.Contract.Status != "canceling" {
		t.Fatalf("unexpected cancel=%#v err=%v", canceled, err)
	}
	if checkoutKey != "billing:checkout" || contractKey != "billing:contract" || changeKey != "billing:change" || portalKey != "billing:portal" || cancelKey != "billing:cancel" {
		t.Fatalf("unexpected idempotency keys: checkout=%q contract=%q change=%q portal=%q cancel=%q", checkoutKey, contractKey, changeKey, portalKey, cancelKey)
	}
}
