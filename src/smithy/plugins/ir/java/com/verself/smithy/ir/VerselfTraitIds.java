package com.verself.smithy.ir;

import software.amazon.smithy.model.shapes.ShapeId;

final class VerselfTraitIds {
  static final ShapeId AUDIT = ShapeId.from("verself.common.v1#audit");
  static final ShapeId AUDIT_EVENT = ShapeId.from("verself.common.v1#auditEvent");
  static final ShapeId AUTHZ = ShapeId.from("verself.common.v1#authz");
  static final ShapeId IDENTITY = ShapeId.from("verself.common.v1#identity");
  static final ShapeId NOT_RESOURCE_PROPERTY =
      ShapeId.from("verself.common.v1#notResourceProperty");
  static final ShapeId PERMISSION = ShapeId.from("verself.common.v1#permission");
  static final ShapeId PROBLEM = ShapeId.from("verself.common.v1#problem");
  static final ShapeId PROTO_FIELD = ShapeId.from("verself.common.v1#protoField");
  static final ShapeId RATE_LIMIT = ShapeId.from("verself.common.v1#rateLimit");
  static final ShapeId REQUEST_BUDGET = ShapeId.from("verself.common.v1#requestBudget");
  static final ShapeId SDK = ShapeId.from("verself.common.v1#sdk");
  static final ShapeId SERVICE_RUNTIME = ShapeId.from("verself.common.v1#serviceRuntime");

  static final String ENUM_VALUE = "smithy.api#enumValue";
  static final String HTTP = "smithy.api#http";
  static final String HTTP_ERROR = "smithy.api#httpError";
  static final String HTTP_HEADER = "smithy.api#httpHeader";
  static final String HTTP_LABEL = "smithy.api#httpLabel";
  static final String HTTP_PAYLOAD = "smithy.api#httpPayload";
  static final String HTTP_QUERY = "smithy.api#httpQuery";
  static final String IDEMPOTENCY_TOKEN = "smithy.api#idempotencyToken";
  static final String IDEMPOTENT = "smithy.api#idempotent";
  static final String INPUT = "smithy.api#input";
  static final String LENGTH = "smithy.api#length";
  static final String MEDIA_TYPE = "smithy.api#mediaType";
  static final String NESTED_PROPERTIES = "smithy.api#nestedProperties";
  static final String OUTPUT = "smithy.api#output";
  static final String PAGINATED = "smithy.api#paginated";
  static final String PATTERN = "smithy.api#pattern";
  static final String RANGE = "smithy.api#range";
  static final String READONLY = "smithy.api#readonly";
  static final String REQUIRED = "smithy.api#required";
  static final String SENSITIVE = "smithy.api#sensitive";
  static final String STREAMING = "smithy.api#streaming";

  private VerselfTraitIds() {}
}
