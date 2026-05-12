# Smithy Models

Smithy files in this directory are the canonical API model. Common Verself
types and traits live under versioned `verself.*.v1` namespaces while service
models add product operations incrementally.

Initial files:

- `verself/common.smithy` — shared primitives, authz/audit/rate-limit/SDK
  traits, RFC 9457 problem shapes, pagination, idempotency, tracing, and future
  protobuf field-number metadata.
- `verself/iam.smithy` — first IAM resource graph and operation set used to
  prove the model shape before generator cutover.
