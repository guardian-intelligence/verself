$version: "2"
namespace verself.billing.v1
use smithy.api#http
use smithy.api#httpHeader
use smithy.api#httpLabel
use smithy.api#httpPayload
use smithy.api#httpQuery
use smithy.api#idempotencyToken
use smithy.api#idempotent
use smithy.api#length
use smithy.api#mediaType
use smithy.api#pattern
use smithy.api#readonly
use smithy.api#required
use verself.common.v1#ConflictError
use verself.common.v1#DisplayName
use verself.common.v1#IdempotencyKey
use verself.common.v1#IdempotencyPayloadMismatchError
use verself.common.v1#PaymentRequiredError
use verself.common.v1#PermissionDeniedError
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
@serviceRuntime(serviceName: "billing-service", publicAudience: "billing-service", internalAudience: "billing-service")
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
        BillingGrant,
        BillingPlan,
        BillingContract,
        BillingDocument
    ]
}
@serviceRuntime(serviceName: "billing-service", publicAudience: "billing-service", internalAudience: "billing-service")
service BillingInternal {
    version: "2026-05-13"
    operations: [
        ReserveWindow,
        ActivateWindow,
        SettleWindow,
        VoidWindow
    ]
    resources: [BillingWindow]
}
@serviceRuntime(serviceName: "billing-service", publicAudience: "billing-service", internalAudience: "billing-service")
service BillingIngress {
    version: "2026-05-13"
    operations: [StripeWebhook]
}
@length(min: 1, max: 128)
string OrgId
@length(min: 1, max: 255)
string ProductId
@length(min: 1, max: 255)
string PlanId
@length(min: 1, max: 255)
string ContractId
@length(min: 1, max: 255)
string DocumentId
@length(min: 1, max: 255)
string BillingWindowId
@length(min: 1, max: 255)
string BillingSourceRef
@length(min: 1, max: 255)
string BillingSourceType
@mediaType("application/json")
blob StripeWebhookPayload
@length(min: 1, max: 255)
string ActorId
@pattern("^[0-9]+$")
string DecimalUint64
@pattern("^-?[0-9]+$")
string DecimalInt64
@length(min: 1, max: 2048)
string URL
@length(min: 1, max: 32)
string Currency
@length(min: 1, max: 128)
string BillingState
@length(min: 1, max: 128)
string BillingMode
@length(min: 1, max: 128)
string BillingCadence
@length(min: 1, max: 128)
string ReservationShape
list BillingGrants {
    member: BillingGrantSummary
}
list BillingPlans {
    member: BillingPlanSummary
}
list BillingContracts {
    member: BillingContractSummary
}
list BillingDocuments {
    member: BillingDocumentSummary
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
resource BillingGrant {}
resource BillingPlan {}
resource BillingContract {}
resource BillingDocument {}
resource BillingWindow {}
resource BillingProviderWebhook {}
structure BillingEntitlementsView {
    @required
    org_id: OrgId
}
structure BillingGrantSummary {
    @required
    grant_id: String
    @required
    available: DecimalUint64
    @required
    pending: DecimalUint64
}
structure BillingPlanSummary {
    @required
    plan_id: PlanId
    @required
    product_id: ProductId
    @required
    display_name: DisplayName
    @required
    billing_mode: BillingMode
    @required
    currency: Currency
    @required
    monthly_amount_cents: DecimalUint64
    @required
    active: Boolean
}
structure BillingContractSummary {
    @required
    contract_id: ContractId
    @required
    product_id: ProductId
    @required
    plan_id: PlanId
    @required
    cadence_kind: BillingCadence
    @required
    status: BillingState
    @required
    starts_at: Timestamp
}
structure BillingDocumentSummary {
    @required
    document_id: DocumentId
    @required
    resourceName: ResourceName
    @required
    product_id: ProductId
    @required
    status: BillingState
    @required
    total_due_units: DecimalUint64
    @required
    currency: Currency
}
structure BillingStatement {
    @required
    org_id: OrgId
    @required
    product_id: ProductId
    @required
    generated_at: Timestamp
    @required
    total_due_units: DecimalUint64
}
structure URLResponse {
    @required
    url: URL
}
structure BillingWindowReservation {
    @required
    window_id: BillingWindowId
    @required
    org_id: OrgId
    @required
    product_id: ProductId
    @required
    actor_id: ActorId
    @required
    source_type: BillingSourceType
    @required
    source_ref: BillingSourceRef
    @required
    reservation_shape: ReservationShape
    @required
    reserved_charge_units: DecimalUint64
    @required
    expires_at: Timestamp
}
structure BillingSettleResult {
    @required
    window_id: BillingWindowId
    @required
    billed_charge_units: DecimalUint64
    @required
    writeoff_charge_units: DecimalUint64
    @required
    settled_at: Timestamp
}
@readonly
@http(method: "GET", uri: "/api/v1/entitlements")
@identity(mode: "bearer", audience: "billing-service", principals: ["browser", "cli", "workload"])
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
    entitlements: BillingEntitlementsView
}
@readonly
@http(method: "GET", uri: "/api/v1/grants")
@identity(mode: "bearer", audience: "billing-service", principals: ["browser", "cli", "workload"])
@authz(permission: BillingReadPermission, organization: {source: "token_org_id"})
@audit(event: BillingGrantListAuditEvent, resource: BillingGrant, action: "list")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "billing.grants", method: "list", paginated: false, retryable: true)
operation ListBillingGrants {
    input: ProductQueryInput
    output: ListBillingGrantsOutput
    errors: [UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}
structure ProductQueryInput {
    @httpQuery("product_id")
    product_id: ProductId
}
structure ListBillingGrantsOutput {
    @required
    grants: BillingGrants
}
@readonly
@http(method: "GET", uri: "/api/v1/billing-documents")
@identity(mode: "bearer", audience: "billing-service", principals: ["browser", "cli", "workload"])
@authz(permission: BillingReadPermission, organization: {source: "token_org_id"})
@audit(event: BillingDocumentListAuditEvent, resource: BillingDocument, action: "list")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "billing.documents", method: "list", paginated: false, retryable: true)
operation ListBillingDocuments {
    input: ProductQueryInput
    output: ListBillingDocumentsOutput
    errors: [UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}
structure ListBillingDocumentsOutput {
    @required
    documents: BillingDocuments
}
@readonly
@http(method: "GET", uri: "/api/v1/statement")
@identity(mode: "bearer", audience: "billing-service", principals: ["browser", "cli", "workload"])
@authz(permission: BillingReadPermission, organization: {source: "token_org_id"})
@audit(event: BillingStatementReadAuditEvent, resource: BillingDocument, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "billing.statement", method: "get", paginated: false, retryable: true)
operation GetBillingStatement {
    input: ProductQueryInput
    output: GetBillingStatementOutput
    errors: [UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}
structure GetBillingStatementOutput {
    @required
    statement: BillingStatement
}
@readonly
@http(method: "GET", uri: "/api/v1/contracts")
@identity(mode: "bearer", audience: "billing-service", principals: ["browser", "cli", "workload"])
@authz(permission: BillingReadPermission, organization: {source: "token_org_id"})
@audit(event: BillingContractListAuditEvent, resource: BillingContract, action: "list")
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
@identity(mode: "bearer", audience: "billing-service", principals: ["browser", "cli", "workload"])
@authz(permission: BillingReadPermission, organization: {source: "token_org_id"})
@audit(event: BillingPlanListAuditEvent, resource: BillingPlan, action: "list")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "billing.plans", method: "list", paginated: false, retryable: true)
operation ListBillingPlans {
    input: EmptyInput
    output: ListBillingPlansOutput
    errors: [UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}
structure ListBillingPlansOutput {
    @required
    plans: BillingPlans
}
@idempotent
@http(method: "POST", uri: "/api/v1/checkout")
@identity(mode: "bearer", audience: "billing-service", principals: ["browser", "cli"])
@authz(permission: BillingCheckoutPermission, organization: {source: "token_org_id"})
@audit(event: BillingCheckoutCreateAuditEvent, resource: BillingEntitlements, action: "create")
@rateLimit(bucket: "billing_mutation")
@requestBudget(maxBytes: 65536)
@sdk(module: "billing.checkout", method: "create", paginated: false, retryable: false)
operation CreateBillingCheckout {
    input: CheckoutInput
    output: URLResponseOutput
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
    amount_cents: Long
    @required
    success_url: URL
    @required
    cancel_url: URL
}
@idempotent
@http(method: "POST", uri: "/api/v1/contracts")
@identity(mode: "bearer", audience: "billing-service", principals: ["browser", "cli"])
@authz(permission: BillingCheckoutPermission, organization: {source: "token_org_id"})
@audit(event: BillingContractCreateAuditEvent, resource: BillingContract, action: "create")
@rateLimit(bucket: "billing_mutation")
@requestBudget(maxBytes: 65536)
@sdk(module: "billing.contracts", method: "create", paginated: false, retryable: false)
operation CreateBillingContract {
    input: CreateBillingContractInput
    output: URLResponseOutput
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
@identity(mode: "bearer", audience: "billing-service", principals: ["browser", "cli"])
@authz(permission: BillingCheckoutPermission, organization: {source: "token_org_id"})
@audit(event: BillingContractChangeCreateAuditEvent, resource: BillingContract, action: "create")
@rateLimit(bucket: "billing_mutation")
@requestBudget(maxBytes: 65536)
@sdk(module: "billing.contracts", method: "createChange", paginated: false, retryable: false)
operation CreateBillingContractChange {
    input: CreateBillingContractChangeInput
    output: URLResponseOutput
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
@idempotent
@http(method: "POST", uri: "/api/v1/contracts/{contract_id}/cancel")
@identity(mode: "bearer", audience: "billing-service", principals: ["browser", "cli"])
@authz(permission: BillingCheckoutPermission, organization: {source: "token_org_id"})
@audit(event: BillingContractCancelAuditEvent, resource: BillingContract, action: "cancel")
@rateLimit(bucket: "billing_mutation")
@requestBudget(maxBytes: 65536)
@sdk(module: "billing.contracts", method: "cancel", paginated: false, retryable: false)
operation CancelBillingContract {
    input: ContractMutationInput
    output: CancelBillingContractOutput
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
structure CancelBillingContractOutput {
    @required
    contract: BillingContractSummary
}
@idempotent
@http(method: "POST", uri: "/api/v1/portal")
@identity(mode: "bearer", audience: "billing-service", principals: ["browser", "cli"])
@authz(permission: BillingCheckoutPermission, organization: {source: "token_org_id"})
@audit(event: BillingPortalCreateAuditEvent, resource: BillingContract, action: "create")
@rateLimit(bucket: "billing_mutation")
@requestBudget(maxBytes: 65536)
@sdk(module: "billing.portal", method: "create", paginated: false, retryable: false)
operation CreateBillingPortal {
    input: CreateBillingPortalInput
    output: URLResponseOutput
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
structure URLResponseOutput {
    @required
    response: URLResponse
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
    source_type: BillingSourceType
    @required
    source_ref: BillingSourceRef
    @required
    reservation_shape: ReservationShape
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
    input: WindowActionInput
    output: ReserveWindowOutput
    errors: [ValidationFailedError, PermissionDeniedError, ResourceNotFoundError, ServiceUnavailableError]
}
@http(method: "POST", uri: "/internal/billing/v1/settle")
@identity(mode: "spiffe_mtls", audience: "billing-service", principals: ["workload"])
@authz(permission: BillingWindowWritePermission, organization: {source: "request_id"})
@audit(event: BillingWindowSettleAuditEvent, resource: BillingWindow, action: "update")
@rateLimit(bucket: "internal_mutation")
@requestBudget(maxBytes: 8192)
@sdk(module: "billingInternal.windows", method: "settle", paginated: false, retryable: false)
operation SettleWindow {
    input: WindowActionInput
    output: SettleWindowOutput
    errors: [ValidationFailedError, PermissionDeniedError, ResourceNotFoundError, ServiceUnavailableError]
}
@http(method: "POST", uri: "/internal/billing/v1/void")
@identity(mode: "spiffe_mtls", audience: "billing-service", principals: ["workload"])
@authz(permission: BillingWindowWritePermission, organization: {source: "request_id"})
@audit(event: BillingWindowVoidAuditEvent, resource: BillingWindow, action: "delete")
@rateLimit(bucket: "internal_mutation")
@requestBudget(maxBytes: 8192)
@sdk(module: "billingInternal.windows", method: "void", paginated: false, retryable: false)
operation VoidWindow {
    input: WindowActionInput
    output: VoidWindowOutput
    errors: [ValidationFailedError, PermissionDeniedError, ResourceNotFoundError, ServiceUnavailableError]
}
structure WindowActionInput {
    @required
    window_id: BillingWindowId
}
structure SettleWindowOutput {
    @required
    settlement: BillingSettleResult
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
    errors: [ValidationFailedError, ServiceUnavailableError]
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
