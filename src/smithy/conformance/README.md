# Conformance

SDK conformance is a shared behavioral test suite generated from the Smithy
model and augmented by hand-authored security cases. Passing conformance means
the SDK agrees with the canonical contract on serialization, authentication
headers, idempotency, pagination, errors, retries, tracing, and version
selection.

Minimum case families:

- request serialization for every operation;
- response and RFC 9457 problem parsing;
- cursor pagination and iterator resume;
- idempotency replay and payload mismatch;
- token refresh, clock skew, audience, and wrong-organization failures;
- 401, 403, 404, 409, 429, and 503 normalization;
- retry behavior from `Retry-After` and modeled retryability;
- W3C `traceparent`, request ID, idempotency key, and SDK user-agent headers.

Live completion evidence for behavior-affecting conformance changes comes from
ClickHouse traces/logs after a deployed rehearsal.
