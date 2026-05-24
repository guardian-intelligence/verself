$version: "2"
namespace verself.billing.v1

use aws.protocols#restJson1

use smithy.api#http
use smithy.api#httpHeader
use smithy.api#httpLabel
use smithy.api#httpPayload
use smithy.api#nestedProperties
use smithy.api#httpQuery
use smithy.api#idempotencyToken
use smithy.api#idempotent
use smithy.api#length
use smithy.api#mediaType
use smithy.api#pattern
use smithy.api#range
use smithy.api#readonly
use smithy.api#required
use verself.common.v1#ConflictError
use verself.common.v1#DisplayName
use verself.common.v1#IdempotencyKey
use verself.common.v1#IdempotencyPayloadMismatchError
use verself.common.v1#PaymentRequiredError
use verself.common.v1#PermissionDeniedError
use verself.common.v1#ProviderWebhookDeliveryReplayConflictError
use verself.common.v1#ProviderWebhookInboxUnavailableError
use verself.common.v1#ProviderWebhookInvalidRequestError
use verself.common.v1#ProviderWebhookSignatureInvalidError
use verself.common.v1#RateLimitedError
use verself.common.v1#ResourceName
use verself.common.v1#ResourceNotFoundError
use verself.common.v1#ServiceUnavailableError
use verself.common.v1#UnauthenticatedError
use verself.common.v1#ValidationFailedError
use verself.common.v1#audit
use verself.common.v1#auditEvent
use verself.common.v1#authz
use verself.common.v1#identity
use verself.common.v1#permission
use verself.common.v1#protoField
use verself.common.v1#rateLimit
use verself.common.v1#requestBudget
use verself.common.v1#sdk
use verself.common.v1#serviceRuntime
use verself.common.v1#DateTime

@serviceRuntime(serviceName: "billing-service", publicAudience: "verself-api", internalAudience: "billing-service")
@restJson1
service Billing {
    version: "2026-05-13"
    operations: [
        GetBillingEntitlements,
        ListBillingGrants,
        ListBillingDocuments,
        GetBillingStatement,
        ListBillingContracts,
        ListBillingPlans,
        CreateBillingCheckout,
        CreateBillingContract,
        CreateBillingContractChange,
        CancelBillingContract,
        CreateBillingPortal
    ]
    resources: [
        BillingEntitlements,
        BillingGrantResource,
        BillingPlanResource,
        BillingContractResource,
        BillingDocumentResource
    ]
}

@serviceRuntime(serviceName: "billing-service", publicAudience: "verself-api", internalAudience: "billing-service")
@restJson1
service BillingInternal {
    version: "2026-05-13"
    operations: [
        GetStorageEntitlement,
        ReserveWindow,
        ActivateWindow,
        SettleWindow,
        VoidWindow
    ]
    resources: [BillingWindow]
}

@serviceRuntime(serviceName: "billing-service", publicAudience: "verself-api", internalAudience: "billing-service")
@restJson1
service BillingIngress {
    version: "2026-05-13"
    operations: [StripeWebhook]
    resources: [BillingProviderWebhook]
}

@pattern("^org_[0-9A-HJKMNP-TV-Z]{26}$")
string OrgId

@length(min: 1, max: 255)
string ProductId

@length(min: 1, max: 255)
string PlanId

@length(min: 1, max: 255)
string ContractId

@length(min: 1, max: 255)
string BillingChangeId

@length(min: 1, max: 255)
string BillingFinalizationId

@length(min: 1, max: 255)
string DocumentId

@length(min: 1, max: 255)
string DocumentNumber

@length(min: 1, max: 255)
string DocumentKind

@length(min: 1, max: 255)
string CycleId

@length(min: 1, max: 255)
string BillingWindowId

@length(min: 1, max: 255)
string BillingSourceRef

@length(min: 1, max: 255)
string BillingSourceType

@length(min: 1, max: 255)
string ActorId

@length(min: 1, max: 255)
string BillingScopeType

@length(max: 255)
string BillingScopeId

@length(max: 255)
string BillingSkuId

@length(max: 255)
string BillingBucketId

@length(max: 255)
string BillingSource

@length(max: 255)
string BillingSourceReferenceId

@length(max: 255)
string BillingEntitlementPeriodId

@length(max: 255)
string BillingPolicyVersion

@pattern("^[0-9]+$")
string DecimalUint64

@pattern("^-?[0-9]+$")
string DecimalInt64

@length(min: 1, max: 8192)
string URL

@length(min: 1, max: 32)
string Currency

@length(min: 1, max: 128)
string BillingState

@length(min: 1, max: 128)
string PaymentState

@length(min: 1, max: 128)
string EntitlementState

@length(min: 1, max: 128)
string BillingMode

@length(min: 1, max: 128)
string BillingCadence

@length(min: 1, max: 128)
string BillingTier

@length(min: 1, max: 128)
string ReservationShape

@length(min: 1, max: 128)
string PricingPhase

@length(min: 1, max: 128)
string QuantityUnit

@length(min: 1, max: 255)
string CoverageLabel

@length(max: 255)
string BillingLabel

@length(max: 255)
string BillingDisplayName

@range(min: 0, max: 9007199254740991)
long SafeUint64

@range(min: -9007199254740991, max: 9007199254740991)
long SafeInt64

@range(min: 1, max: 9007199254740991)
long CheckoutAmountCents

@range(min: 0, max: 2147483647)
integer WindowSequence

@range(min: 0, max: 4294967295)
long WindowQuantity

@mediaType("application/json")
blob StripeWebhookPayload

list BillingGrants {
    member: BillingGrant
}

list BillingPlans {
    member: BillingPlan
}

list BillingContracts {
    member: BillingContract
}

list BillingDocuments {
    member: BillingDocument
}

list BillingStatementLineItems {
    member: BillingStatementLineItem
}

list BillingStatementGrantSummaries {
    member: BillingStatementGrantSummary
}

list BillingEntitlementSourceTotals {
    member: BillingEntitlementSourceTotal
}

list BillingEntitlementProductSections {
    member: BillingEntitlementProductSection
}

list BillingEntitlementBucketSections {
    member: BillingEntitlementBucketSection
}

list BillingEntitlementSlots {
    member: BillingEntitlementSlot
}

map BillingAllocation {
    key: String
    value: Double
}

map BillingSKURates {
    key: String
    value: DecimalUint64
}

@permission(name: "billing:read")
string BillingReadPermission

@permission(name: "billing:checkout")
string BillingCheckoutPermission

@permission(name: "billing:window:write")
string BillingWindowWritePermission

@permission(name: "billing:provider_webhook:receive")
string BillingProviderWebhookPermission

@auditEvent(name: "billing.entitlements.read")
string BillingEntitlementsReadAuditEvent

@auditEvent(name: "billing.grant.list")
string BillingGrantListAuditEvent

@auditEvent(name: "billing.document.list")
string BillingDocumentListAuditEvent

@auditEvent(name: "billing.statement.read")
string BillingStatementReadAuditEvent

@auditEvent(name: "billing.contract.list")
string BillingContractListAuditEvent

@auditEvent(name: "billing.plan.list")
string BillingPlanListAuditEvent

@auditEvent(name: "billing.checkout.create")
string BillingCheckoutCreateAuditEvent

@auditEvent(name: "billing.contract_checkout.create")
string BillingContractCreateAuditEvent

@auditEvent(name: "billing.contract_change.create")
string BillingContractChangeCreateAuditEvent

@auditEvent(name: "billing.contract.cancel")
string BillingContractCancelAuditEvent

@auditEvent(name: "billing.portal.create")
string BillingPortalCreateAuditEvent

@auditEvent(name: "billing.window.reserve")
string BillingWindowReserveAuditEvent

@auditEvent(name: "billing.window.activate")
string BillingWindowActivateAuditEvent

@auditEvent(name: "billing.window.settle")
string BillingWindowSettleAuditEvent

@auditEvent(name: "billing.window.void")
string BillingWindowVoidAuditEvent

@auditEvent(name: "billing.stripe.webhook")
string StripeWebhookAuditEvent

resource BillingEntitlements {}
resource BillingGrantResource {}
resource BillingPlanResource {}
resource BillingContractResource {}
resource BillingDocumentResource {}
resource BillingWindow {}
resource BillingProviderWebhook {}

structure BillingGrant {
    @required
    grant_id: String
    @required
    scope_type: BillingScopeType
    @required
    scope_product_id: BillingScopeId
    @required
    scope_bucket_id: BillingScopeId
    @required
    scope_sku_id: BillingSkuId
    @required
    source: BillingSource
    @required
    source_reference_id: BillingSourceReferenceId
    @required
    entitlement_period_id: BillingEntitlementPeriodId
    @required
    policy_version: BillingPolicyVersion
    @required
    starts_at: DateTime
    period_start: DateTime
    period_end: DateTime
    @required
    available: DecimalUint64
    @required
    pending: DecimalUint64
    expires_at: DateTime
}

structure BillingPlan {
    @required
    plan_id: PlanId
    @required
    product_id: ProductId
    @required
    display_name: DisplayName
    @required
    billing_mode: BillingMode
    @required
    tier: BillingTier
    @required
    currency: Currency
    @required
    monthly_amount_cents: DecimalUint64
    @required
    annual_amount_cents: DecimalUint64
    @required
    active: Boolean
    @required
    is_default: Boolean
}

structure BillingContract {
    @required
    contract_id: ContractId
    @required
    product_id: ProductId
    @required
    plan_id: PlanId
    @required
    phase_id: String
    @required
    cadence_kind: BillingCadence
    @required
    status: BillingState
    @required
    payment_state: PaymentState
    @required
    entitlement_state: EntitlementState
    pending_change_id: BillingChangeId
    pending_change_type: BillingState
    pending_change_target_plan_id: PlanId
    pending_change_effective_at: DateTime
    @required
    starts_at: DateTime
    ends_at: DateTime
    phase_start: DateTime
    phase_end: DateTime
}

structure BillingDocument {
    @required
    document_id: DocumentId
    @required
    resourceName: ResourceName
    @required
    document_number: DocumentNumber
    @required
    document_kind: DocumentKind
    @required
    finalization_id: BillingFinalizationId
    @required
    product_id: ProductId
    @required
    cycle_id: CycleId
    @required
    status: BillingState
    @required
    payment_status: PaymentState
    @required
    period_start: DateTime
    @required
    period_end: DateTime
    issued_at: DateTime
    @required
    currency: Currency
    @required
    subtotal_units: DecimalUint64
    @required
    adjustment_units: DecimalInt64
    @required
    tax_units: DecimalUint64
    @required
    total_due_units: DecimalUint64
    stripe_hosted_invoice_url: URL
    stripe_invoice_pdf_url: URL
    stripe_payment_intent_id: String
}

structure BillingEntitlementsView {
    @required
    org_id: OrgId
    @required
    durable_storage_quota_bytes: SafeUint64
    @required
    universal: BillingEntitlementSlot
    @required
    products: BillingEntitlementProductSections
}

structure BillingEntitlementProductSection {
    @required
    product_id: ProductId
    @required
    display_name: DisplayName
    product_slot: BillingEntitlementSlot
    @required
    buckets: BillingEntitlementBucketSections
}

structure BillingEntitlementBucketSection {
    @required
    bucket_id: BillingBucketId
    @required
    display_name: DisplayName
    bucket_slot: BillingEntitlementSlot
    @required
    sku_slots: BillingEntitlementSlots
}

structure BillingEntitlementSlot {
    @required
    scope_type: BillingScopeType
    @required
    product_id: BillingScopeId
    @required
    product_display: BillingDisplayName
    @required
    bucket_id: BillingScopeId
    @required
    bucket_display: BillingDisplayName
    @required
    sku_id: BillingSkuId
    @required
    sku_display: BillingDisplayName
    @required
    coverage_label: CoverageLabel
    @required
    period_start_units: DecimalUint64
    @required
    spent_units: DecimalUint64
    @required
    pending_units: DecimalUint64
    @required
    available_units: DecimalUint64
    @required
    sources: BillingEntitlementSourceTotals
}

structure BillingEntitlementSourceTotal {
    @required
    source: BillingSource
    @required
    plan_id: BillingScopeId
    @required
    label: BillingLabel
    @required
    period_start_units: DecimalUint64
    @required
    available_units: DecimalUint64
    @required
    pending_units: DecimalUint64
    inline_expires_at: DateTime
}

structure BillingStatement {
    @required
    org_id: OrgId
    @required
    product_id: ProductId
    @required
    period_start: DateTime
    @required
    period_end: DateTime
    @required
    period_source: String
    @required
    generated_at: DateTime
    @required
    currency: Currency
    @required
    unit_label: String
    @required
    line_items: BillingStatementLineItems
    @required
    grant_summaries: BillingStatementGrantSummaries
    @required
    totals: BillingStatementTotals
}

structure BillingStatementLineItem {
    @required
    product_id: ProductId
    @required
    plan_id: BillingScopeId
    @required
    bucket_id: BillingBucketId
    @required
    bucket_display_name: BillingDisplayName
    @required
    sku_id: BillingSkuId
    @required
    sku_display_name: BillingDisplayName
    @required
    quantity_unit: QuantityUnit
    @required
    pricing_phase: PricingPhase
    @required
    quantity: Double
    @required
    unit_rate: DecimalUint64
    @required
    charge_units: DecimalUint64
    @required
    free_tier_units: DecimalUint64
    @required
    contract_units: DecimalUint64
    @required
    purchase_units: DecimalUint64
    @required
    promo_units: DecimalUint64
    @required
    refund_units: DecimalUint64
    @required
    receivable_units: DecimalUint64
    @required
    reserved_units: DecimalUint64
}

structure BillingStatementGrantSummary {
    @required
    scope_type: BillingScopeType
    @required
    scope_product_id: BillingScopeId
    @required
    scope_bucket_id: BillingScopeId
    @required
    source: BillingSource
    @required
    available: DecimalUint64
    @required
    pending: DecimalUint64
}

structure BillingStatementTotals {
    @required
    charge_units: DecimalUint64
    @required
    free_tier_units: DecimalUint64
    @required
    contract_units: DecimalUint64
    @required
    purchase_units: DecimalUint64
    @required
    promo_units: DecimalUint64
    @required
    refund_units: DecimalUint64
    @required
    receivable_units: DecimalUint64
    @required
    reserved_units: DecimalUint64
    @required
    total_due_units: DecimalUint64
}

structure BillingURLResponse {
    @required
    url: URL
}

structure BillingContractChangeResponse {
    @required
    url: URL
    @required
    change_id: BillingChangeId
    finalization_id: BillingFinalizationId
    document_id: DocumentId
    @required
    status: BillingState
    @required
    price_delta_units: DecimalUint64
}

structure BillingCancelContractResponse {
    @required
    contract: BillingContract
}

structure BillingWindowReservation {
    @required
    window_id: BillingWindowId
    @required
    org_id: OrgId
    @required
    product_id: ProductId
    @required
    plan_id: PlanId
    @required
    actor_id: ActorId
    @required
    source_type: BillingSourceType
    @required
    source_ref: BillingSourceRef
    @required
    window_seq: WindowSequence
    @required
    reservation_shape: ReservationShape
    @required
    reserved_quantity: WindowQuantity
    @required
    reserved_charge_units: DecimalUint64
    @required
    pricing_phase: PricingPhase
    @required
    allocation: BillingAllocation
    @required
    sku_rates: BillingSKURates
    @required
    cost_per_unit: DecimalUint64
    @required
    window_start: DateTime
    activated_at: DateTime
    @required
    expires_at: DateTime
    renew_by: DateTime
}

structure BillingSettleResult {
    @required
    window_id: BillingWindowId
    @required
    actual_quantity: WindowQuantity
    @required
    billable_quantity: WindowQuantity
    @required
    writeoff_quantity: WindowQuantity
    @required
    billed_charge_units: DecimalUint64
    @required
    writeoff_charge_units: DecimalUint64
    @required
    settled_at: DateTime
}

@readonly
@http(method: "GET", uri: "/api/v1/entitlements")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli", "workload"])
@authz(permission: BillingReadPermission, organization: {source: "token_org_id"})
@audit(event: BillingEntitlementsReadAuditEvent, resource: BillingEntitlements, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "billing.entitlements", method: "get", paginated: false, retryable: true)
operation GetBillingEntitlements {
    input: EmptyInput
    output: GetBillingEntitlementsOutput
    errors: [UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}

structure EmptyInput {}

structure GetBillingEntitlementsOutput {
    @required
    @httpPayload
    @nestedProperties
    entitlements: BillingEntitlementsView
}

@readonly
@http(method: "GET", uri: "/api/v1/grants")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli", "workload"])
@authz(permission: BillingReadPermission, organization: {source: "token_org_id"})
@audit(event: BillingGrantListAuditEvent, resource: BillingGrantResource, action: "list")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "billing.grants", method: "list", paginated: false, retryable: true)
operation ListBillingGrants {
    input: GrantsQueryInput
    output: ListBillingGrantsOutput
    errors: [UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}

structure GrantsQueryInput {
    @httpQuery("product_id")
    product_id: ProductId
    @httpQuery("active")
    active: Boolean
}

structure ListBillingGrantsOutput {
    @required
    grants: BillingGrants
}

@readonly
@http(method: "GET", uri: "/api/v1/billing-documents")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli", "workload"])
@authz(permission: BillingReadPermission, organization: {source: "token_org_id"})
@audit(event: BillingDocumentListAuditEvent, resource: BillingDocumentResource, action: "list")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "billing.documents", method: "list", paginated: false, retryable: true)
operation ListBillingDocuments {
    input: DocumentsQueryInput
    output: ListBillingDocumentsOutput
    errors: [UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}

structure DocumentsQueryInput {
    @httpQuery("product_id")
    product_id: ProductId
}

structure ListBillingDocumentsOutput {
    @required
    documents: BillingDocuments
}

@readonly
@http(method: "GET", uri: "/api/v1/statement")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli", "workload"])
@authz(permission: BillingReadPermission, organization: {source: "token_org_id"})
@audit(event: BillingStatementReadAuditEvent, resource: BillingDocumentResource, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "billing.statement", method: "get", paginated: false, retryable: true)
operation GetBillingStatement {
    input: ProductQueryInput
    output: GetBillingStatementOutput
    errors: [UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}

structure ProductQueryInput {
    @required
    @httpQuery("product_id")
    product_id: ProductId
}

structure GetBillingStatementOutput {
    @required
    @httpPayload
    @nestedProperties
    statement: BillingStatement
}

@readonly
@http(method: "GET", uri: "/api/v1/contracts")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli", "workload"])
@authz(permission: BillingReadPermission, organization: {source: "token_org_id"})
@audit(event: BillingContractListAuditEvent, resource: BillingContractResource, action: "list")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "billing.contracts", method: "list", paginated: false, retryable: true)
operation ListBillingContracts {
    input: EmptyInput
    output: ListBillingContractsOutput
    errors: [UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}

structure ListBillingContractsOutput {
    @required
    contracts: BillingContracts
}

@readonly
@http(method: "GET", uri: "/api/v1/plans")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli", "workload"])
@authz(permission: BillingReadPermission, organization: {source: "token_org_id"})
@audit(event: BillingPlanListAuditEvent, resource: BillingPlanResource, action: "list")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "billing.plans", method: "list", paginated: false, retryable: true)
operation ListBillingPlans {
    input: ProductQueryInput
    output: ListBillingPlansOutput
    errors: [UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}

structure ListBillingPlansOutput {
    @required
    plans: BillingPlans
}

@idempotent
@http(method: "POST", uri: "/api/v1/checkout")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: BillingCheckoutPermission, organization: {source: "token_org_id"})
@audit(event: BillingCheckoutCreateAuditEvent, resource: BillingEntitlements, action: "create")
@rateLimit(bucket: "billing_mutation")
@requestBudget(maxBytes: 65536)
@sdk(module: "billing.checkout", method: "create", paginated: false, retryable: false)
operation CreateBillingCheckout {
    input: CheckoutInput
    output: BillingURLResponseOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}

structure CheckoutInput {
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey
    @required
    product_id: ProductId
    @required
    amount_cents: CheckoutAmountCents
    @required
    success_url: URL
    @required
    cancel_url: URL
}

@idempotent
@http(method: "POST", uri: "/api/v1/contracts")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: BillingCheckoutPermission, organization: {source: "token_org_id"})
@audit(event: BillingContractCreateAuditEvent, resource: BillingContractResource, action: "create")
@rateLimit(bucket: "billing_mutation")
@requestBudget(maxBytes: 65536)
@sdk(module: "billing.contracts", method: "create", paginated: false, retryable: false)
operation CreateBillingContract {
    input: CreateBillingContractInput
    output: BillingURLResponseOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}

structure CreateBillingContractInput {
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey
    @required
    plan_id: PlanId
    cadence: BillingCadence
    @required
    success_url: URL
    @required
    cancel_url: URL
}

@idempotent
@http(method: "POST", uri: "/api/v1/contracts/{contract_id}/changes")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: BillingCheckoutPermission, organization: {source: "token_org_id"})
@audit(event: BillingContractChangeCreateAuditEvent, resource: BillingContractResource, action: "create")
@rateLimit(bucket: "billing_mutation")
@requestBudget(maxBytes: 65536)
@sdk(module: "billing.contracts", method: "createChange", paginated: false, retryable: false)
operation CreateBillingContractChange {
    input: CreateBillingContractChangeInput
    output: CreateBillingContractChangeOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}

structure CreateBillingContractChangeInput {
    @required
    @httpLabel
    contract_id: ContractId
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey
    @required
    target_plan_id: PlanId
    @required
    success_url: URL
    @required
    cancel_url: URL
}

structure CreateBillingContractChangeOutput {
    @required
    @httpPayload
    @nestedProperties
    change: BillingContractChangeResponse
}

@idempotent
@http(method: "POST", uri: "/api/v1/contracts/{contract_id}/cancel")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: BillingCheckoutPermission, organization: {source: "token_org_id"})
@audit(event: BillingContractCancelAuditEvent, resource: BillingContractResource, action: "cancel")
@rateLimit(bucket: "billing_mutation")
@requestBudget(maxBytes: 65536)
@sdk(module: "billing.contracts", method: "cancel", paginated: false, retryable: false)
operation CancelBillingContract {
    input: ContractMutationInput
    output: BillingCancelContractResponseOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}

structure ContractMutationInput {
    @required
    @httpLabel
    contract_id: ContractId
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey
}

structure BillingCancelContractResponseOutput {
    @required
    contract: BillingContract
}

@idempotent
@http(method: "POST", uri: "/api/v1/portal")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: BillingCheckoutPermission, organization: {source: "token_org_id"})
@audit(event: BillingPortalCreateAuditEvent, resource: BillingContractResource, action: "create")
@rateLimit(bucket: "billing_mutation")
@requestBudget(maxBytes: 65536)
@sdk(module: "billing.portal", method: "create", paginated: false, retryable: false)
operation CreateBillingPortal {
    input: CreateBillingPortalInput
    output: BillingURLResponseOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}

structure CreateBillingPortalInput {
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey
    @required
    return_url: URL
}

structure BillingURLResponseOutput {
    @required
    @httpPayload
    @nestedProperties
    response: BillingURLResponse
}

@idempotent
@http(method: "POST", uri: "/internal/billing/v1/storage-entitlement")
@identity(mode: "spiffe_mtls", audience: "billing-service", principals: ["workload"])
@authz(permission: BillingReadPermission, organization: {source: "body_org_id", member: "org_id"})
@audit(event: BillingEntitlementsReadAuditEvent, resource: BillingEntitlements, action: "read")
@rateLimit(bucket: "internal_read")
@requestBudget(maxBytes: 65536)
@sdk(module: "billingInternal.entitlements", method: "getStorage", paginated: false, retryable: true)
operation GetStorageEntitlement {
    input: GetStorageEntitlementInput
    output: GetStorageEntitlementOutput
    errors: [ValidationFailedError, PermissionDeniedError, ServiceUnavailableError]
}

structure GetStorageEntitlementInput {
    @required
    org_id: OrgId
    @required
    product_id: ProductId
}

structure GetStorageEntitlementOutput {
    @required
    @httpPayload
    @nestedProperties
    entitlement: BillingStorageEntitlement
}

structure BillingStorageEntitlement {
    @required
    org_id: OrgId
    @required
    product_id: ProductId
    @required
    plan_id: BillingScopeId
    @required
    plan_tier: BillingTier
    @required
    durable_storage_quota_bytes: SafeUint64
}

@http(method: "POST", uri: "/internal/billing/v1/reserve")
@identity(mode: "spiffe_mtls", audience: "billing-service", principals: ["workload"])
@authz(permission: BillingWindowWritePermission, organization: {source: "body_org_id", member: "org_id"})
@audit(event: BillingWindowReserveAuditEvent, resource: BillingWindow, action: "create")
@rateLimit(bucket: "internal_mutation")
@requestBudget(maxBytes: 65536)
@sdk(module: "billingInternal.windows", method: "reserve", paginated: false, retryable: false)
operation ReserveWindow {
    input: ReserveWindowInput
    output: ReserveWindowOutput
    errors: [ValidationFailedError, PermissionDeniedError, PaymentRequiredError, ServiceUnavailableError]
}

structure ReserveWindowInput {
    @required
    org_id: OrgId
    @required
    product_id: ProductId
    @required
    actor_id: ActorId
    @required
    concurrent_count: SafeUint64
    @required
    source_type: BillingSourceType
    @required
    source_ref: BillingSourceRef
    @required
    window_seq: WindowSequence
    @required
    reservation_shape: ReservationShape
    @required
    reserved_quantity: WindowQuantity
    billing_job_id: SafeInt64
    @required
    allocation: BillingAllocation
}

structure ReserveWindowOutput {
    @required
    reservation: BillingWindowReservation
}

@http(method: "POST", uri: "/internal/billing/v1/activate")
@identity(mode: "spiffe_mtls", audience: "billing-service", principals: ["workload"])
@authz(permission: BillingWindowWritePermission, organization: {source: "request_id"})
@audit(event: BillingWindowActivateAuditEvent, resource: BillingWindow, action: "update")
@rateLimit(bucket: "internal_mutation")
@requestBudget(maxBytes: 8192)
@sdk(module: "billingInternal.windows", method: "activate", paginated: false, retryable: false)
operation ActivateWindow {
    input: ActivateWindowInput
    output: ReserveWindowOutput
    errors: [ValidationFailedError, PermissionDeniedError, ResourceNotFoundError, ServiceUnavailableError]
}

structure ActivateWindowInput {
    @required
    window_id: BillingWindowId
    @required
    activated_at: DateTime
}

@http(method: "POST", uri: "/internal/billing/v1/settle")
@identity(mode: "spiffe_mtls", audience: "billing-service", principals: ["workload"])
@authz(permission: BillingWindowWritePermission, organization: {source: "request_id"})
@audit(event: BillingWindowSettleAuditEvent, resource: BillingWindow, action: "update")
@rateLimit(bucket: "internal_mutation")
@requestBudget(maxBytes: 8192)
@sdk(module: "billingInternal.windows", method: "settle", paginated: false, retryable: false)
operation SettleWindow {
    input: SettleWindowInput
    output: SettleWindowOutput
    errors: [ValidationFailedError, PermissionDeniedError, ResourceNotFoundError, ServiceUnavailableError]
}

structure SettleWindowInput {
    @required
    window_id: BillingWindowId
    @required
    actual_quantity: WindowQuantity
    usage_summary: Document
}

structure SettleWindowOutput {
    @required
    @httpPayload
    @nestedProperties
    settlement: BillingSettleResult
}

@http(method: "POST", uri: "/internal/billing/v1/void")
@identity(mode: "spiffe_mtls", audience: "billing-service", principals: ["workload"])
@authz(permission: BillingWindowWritePermission, organization: {source: "request_id"})
@audit(event: BillingWindowVoidAuditEvent, resource: BillingWindow, action: "delete")
@rateLimit(bucket: "internal_mutation")
@requestBudget(maxBytes: 8192)
@sdk(module: "billingInternal.windows", method: "void", paginated: false, retryable: false)
operation VoidWindow {
    input: VoidWindowInput
    output: VoidWindowOutput
    errors: [ValidationFailedError, PermissionDeniedError, ResourceNotFoundError, ServiceUnavailableError]
}

structure VoidWindowInput {
    @required
    window_id: BillingWindowId
}

structure VoidWindowOutput {
    @required
    window_id: BillingWindowId
}

@http(method: "POST", uri: "/webhooks/stripe")
@identity(mode: "provider_webhook", audience: "billing-service", principals: ["provider"])
@authz(permission: BillingProviderWebhookPermission, organization: {source: "request_id"})
@audit(event: StripeWebhookAuditEvent, resource: BillingProviderWebhook, action: "write")
@rateLimit(bucket: "provider_webhook")
@requestBudget(maxBytes: 262144)
@sdk(module: "billingIngress.stripe", method: "webhook", paginated: false, retryable: false)
operation StripeWebhook {
    input: StripeWebhookInput
    output: StripeWebhookOutput
    errors: [
        ProviderWebhookInvalidRequestError,
        ProviderWebhookSignatureInvalidError,
        ProviderWebhookDeliveryReplayConflictError,
        ProviderWebhookInboxUnavailableError,
        ServiceUnavailableError
    ]
}

structure StripeWebhookInput {
    @required
    @httpHeader("Stripe-Signature")
    signature: String
    @required
    @httpPayload
    body: StripeWebhookPayload
}

structure StripeWebhookOutput {}
