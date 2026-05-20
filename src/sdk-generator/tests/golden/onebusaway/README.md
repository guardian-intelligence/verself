# Golden tape-out target — hand-written, NOT generated

Purpose (advisor design-twice on the highest-stakes interface): this is the exact
generated code we want `emit.python` (slice 5) to produce for the OneBusAway `agency`
resource, **hand-written first** so the runtime (slice 4) is shaped by a real consumer
instead of designed in the abstract.

Build order this enforces:
1. this target (done) →
2. `src/stainful/runtime/` written to satisfy *exactly* the `_core` API it imports →
3. emitter written to reproduce these bytes from the IR.

Scope: the artifact's job is the **runtime↔emitter boundary + emitted code shape**.
Deep model-graph fidelity is already proven by the slice-3 IR tests, so `Reference`'s
fan-out is represented at the boundary (typed `list[object]` with `# emitter:
ModelRef(X)` markers) rather than hand-transcribing Route/Stop/Trip/Situation.

## The runtime contract this pins (the slice-4 spec)

`onebusaway._core` must expose, with these exact symbols:

- `_sentinels`: `NotGiven`, `not_given`, `NOT_GIVEN` (alias), `Omit`, `omit`
- `_types`: `Headers`, `Query`, `Body`, `Omittable`
- `_models`: `BaseModel` (pydantic v2 base; carries `_request_id`)
- `_resource`: `SyncAPIResource`, `AsyncAPIResource` (hold a client; expose
  `_get/_post/_put/_patch/_delete/_get_api_list`)
- `_request_options`: `make_request_options(...)`, `RequestOptions`
- `_response`: `to_raw_response_wrapper`, `async_to_raw_response_wrapper`,
  `to_streamed_response_wrapper`, `async_to_streamed_response_wrapper`
- `_base_client`: `SyncAPIClient`, `AsyncAPIClient` (retries, auth injection,
  error mapping, env-var key)
- `_exceptions`: `APIError`, `APIStatusError`, `BadRequestError`,
  `AuthenticationError`, `PermissionDeniedError`, `NotFoundError`,
  `ConflictError`, `UnprocessableEntityError`, `RateLimitError`,
  `InternalServerError`, `APIConnectionError`, `APITimeoutError`,
  `APIResponseValidationError`
- `pagination`: `SyncPage`, `AsyncPage`, `SyncCursorPage`, `AsyncCursorPage`,
  `PageInfo` (unused by OneBusAway but part of the §4 bar)
