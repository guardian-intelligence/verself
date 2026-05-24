package com.verself.smithy.plugins;

import java.util.ArrayList;
import java.util.Comparator;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import software.amazon.smithy.build.PluginContext;
import software.amazon.smithy.build.SmithyBuildPlugin;
import software.amazon.smithy.model.Model;
import software.amazon.smithy.model.node.ArrayNode;
import software.amazon.smithy.model.node.Node;
import software.amazon.smithy.model.node.ObjectNode;
import software.amazon.smithy.model.node.StringNode;
import software.amazon.smithy.model.shapes.MemberShape;
import software.amazon.smithy.model.shapes.OperationShape;
import software.amazon.smithy.model.shapes.ServiceShape;
import software.amazon.smithy.model.shapes.Shape;
import software.amazon.smithy.model.shapes.ShapeId;
import software.amazon.smithy.model.shapes.StructureShape;
import software.amazon.smithy.model.traits.Trait;

/** Generates compact runtime route catalogs from Smithy operation traits. */
public final class VerselfRouteCatalogPlugin implements SmithyBuildPlugin {
  private static final String PLUGIN_NAME = "verselfRouteCatalog";
  private static final String SCHEMA_VERSION = "verself.route-catalog.v1";
  private static final String VERSELF_NAMESPACE_PREFIX = "verself.";
  private static final String COMMON_NAMESPACE = "verself.common.v1";

  private static final ShapeId AUDIT = ShapeId.from("verself.common.v1#audit");
  private static final ShapeId AUDIT_EVENT = ShapeId.from("verself.common.v1#auditEvent");
  private static final ShapeId AUTHZ = ShapeId.from("verself.common.v1#authz");
  private static final ShapeId IDENTITY = ShapeId.from("verself.common.v1#identity");
  private static final ShapeId OPERATION_SEMANTICS =
      ShapeId.from("verself.common.v1#operationSemantics");
  private static final ShapeId PERMISSION = ShapeId.from("verself.common.v1#permission");
  private static final ShapeId PROBLEM = ShapeId.from("verself.common.v1#problem");
  private static final ShapeId RATE_LIMIT = ShapeId.from("verself.common.v1#rateLimit");
  private static final ShapeId REQUEST_BUDGET = ShapeId.from("verself.common.v1#requestBudget");
  private static final ShapeId SDK = ShapeId.from("verself.common.v1#sdk");
  private static final ShapeId SERVICE_RUNTIME = ShapeId.from("verself.common.v1#serviceRuntime");

  private static final ShapeId HTTP = ShapeId.from("smithy.api#http");
  private static final ShapeId HTTP_ERROR = ShapeId.from("smithy.api#httpError");
  private static final ShapeId HTTP_HEADER = ShapeId.from("smithy.api#httpHeader");
  private static final ShapeId IDEMPOTENCY_TOKEN = ShapeId.from("smithy.api#idempotencyToken");
  private static final ShapeId PAGINATED = ShapeId.from("smithy.api#paginated");
  private static final ShapeId READONLY = ShapeId.from("smithy.api#readonly");

  @Override
  public String getName() {
    return PLUGIN_NAME;
  }

  @Override
  public void execute(PluginContext context) {
    Model model = context.getModel();
    List<Map<String, Object>> catalogs = new ArrayList<>();

    for (ServiceShape service : sorted(model.getServiceShapes())) {
      if (!isVerselfService(service)) {
        continue;
      }
      Map<String, Object> catalog = serviceCatalog(model, context.getProjectionName(), service);
      catalogs.add(catalog);
      context
          .getFileManifest()
          .writeFile(
              "route-catalog/" + service.getId().getName() + ".json", toJson(catalog) + "\n");
    }

    Map<String, Object> aggregate = new LinkedHashMap<>();
    aggregate.put("schema_version", SCHEMA_VERSION);
    aggregate.put("projection", context.getProjectionName());
    aggregate.put("services", catalogs);
    context.getFileManifest().writeFile("route-catalog/all.json", toJson(aggregate) + "\n");
  }

  private static Map<String, Object> serviceCatalog(
      Model model, String projection, ServiceShape service) {
    ObjectNode runtime = objectTrait(service, SERVICE_RUNTIME).orElseThrow();
    List<Map<String, Object>> operations = new ArrayList<>();
    for (ShapeId operationId : service.getAllOperations()) {
      OperationShape operation = model.expectShape(operationId).asOperationShape().orElseThrow();
      operations.add(operationCatalog(model, operation));
    }
    operations.sort(Comparator.comparing(op -> op.get("operation_id").toString()));

    Map<String, Object> serviceNode = new LinkedHashMap<>();
    serviceNode.put("shape_id", service.getId().toString());
    serviceNode.put("name", stringMember(runtime, "serviceName").orElse(""));
    serviceNode.put("version", service.getVersion());
    serviceNode.put("public_audience", stringMember(runtime, "publicAudience").orElse(""));
    serviceNode.put("internal_audience", stringMember(runtime, "internalAudience").orElse(""));

    Map<String, Object> catalog = new LinkedHashMap<>();
    catalog.put("schema_version", SCHEMA_VERSION);
    catalog.put("projection", projection);
    catalog.put("service", serviceNode);
    catalog.put("operations", operations);
    return catalog;
  }

  private static Map<String, Object> operationCatalog(Model model, OperationShape operation) {
    ObjectNode http = objectTrait(operation, HTTP).orElseThrow();
    ObjectNode identity = objectTrait(operation, IDENTITY).orElseThrow();
    ObjectNode authz = objectTrait(operation, AUTHZ).orElseThrow();
    ObjectNode audit = objectTrait(operation, AUDIT).orElseThrow();
    ObjectNode rateLimit = objectTrait(operation, RATE_LIMIT).orElseThrow();
    ObjectNode requestBudget = objectTrait(operation, REQUEST_BUDGET).orElseThrow();
    ObjectNode sdk = objectTrait(operation, SDK).orElseThrow();

    Map<String, Object> result = new LinkedHashMap<>();
    result.put("shape_id", operation.getId().toString());
    result.put("operation_id", kebabCase(operation.getId().getName()));
    result.put("method", stringMember(http, "method").orElse(""));
    result.put("path", stringMember(http, "uri").orElse(""));
    result.put("default_status", longMember(http, "code").orElse(200L));
    result.put("input_shape", operation.getInput().map(ShapeId::toString).orElse(""));
    result.put("output_shape", operation.getOutput().map(ShapeId::toString).orElse(""));
    result.put("effect", operationEffect(operation));
    result.put("paginated", operation.hasTrait(PAGINATED));
    result.put("identity", identityCatalog(identity));
    result.put("authorization", authorizationCatalog(model, authz));
    result.put("audit", auditCatalog(model, audit));
    result.put("rate_limit", Map.of("bucket", stringMember(rateLimit, "bucket").orElse("")));
    result.put(
        "request_body", Map.of("max_bytes", longMember(requestBudget, "maxBytes").orElse(0L)));
    result.put("idempotency", idempotencyCatalog(model, operation));
    result.put("sdk", sdkCatalog(sdk));
    result.put("errors", errorCatalogs(model, operation));
    return result;
  }

  private static Map<String, Object> identityCatalog(ObjectNode identity) {
    Map<String, Object> result = new LinkedHashMap<>();
    result.put("mode", stringMember(identity, "mode").orElse(""));
    result.put("audience", stringMember(identity, "audience").orElse(""));
    result.put("principals", stringListMember(identity, "principals"));
    return result;
  }

  private static Map<String, Object> authorizationCatalog(Model model, ObjectNode authz) {
    Optional<ObjectNode> organization = authz.getObjectMember("organization");
    ShapeId permission = shapeIdMember(authz, "permission").orElseThrow();

    Map<String, Object> result = new LinkedHashMap<>();
    result.put("permission", permissionName(model, permission));
    result.put(
        "organization_source",
        organization.flatMap(node -> stringMember(node, "source")).orElse(""));
    result.put(
        "organization_member",
        organization.flatMap(node -> stringMember(node, "member")).orElse(""));
    return result;
  }

  private static Map<String, Object> auditCatalog(Model model, ObjectNode audit) {
    ShapeId event = shapeIdMember(audit, "event").orElseThrow();
    ShapeId resource = shapeIdMember(audit, "resource").orElseThrow();

    Map<String, Object> result = new LinkedHashMap<>();
    result.put("event", auditEventName(model, event));
    result.put("resource", snakeCase(resource.getName()));
    result.put("action", stringMember(audit, "action").orElse(""));
    result.put("ocsf_class_uid", longMember(audit, "ocsf_class_uid").orElse(0L));
    result.put("ocsf_class_name", stringMember(audit, "ocsf_class_name").orElse(""));
    return result;
  }

  private static Map<String, Object> idempotencyCatalog(Model model, OperationShape operation) {
    Optional<StructureShape> input =
        operation.getInput().flatMap(model::getShape).flatMap(Shape::asStructureShape);
    Optional<MemberShape> token =
        input.flatMap(
            shape ->
                shape.getAllMembers().values().stream()
                    .filter(member -> member.hasTrait(IDEMPOTENCY_TOKEN))
                    .findFirst());

    Map<String, Object> result = new LinkedHashMap<>();
    if (token.isEmpty()) {
      result.put("policy", "");
      result.put("header", "");
      result.put("member", "");
      return result;
    }
    result.put(
        "policy",
        token.get().hasTrait(HTTP_HEADER)
            ? "idempotency_key_header"
            : "request_body_idempotency_key");
    result.put("header", stringTrait(token.get(), HTTP_HEADER).orElse(""));
    result.put("member", token.get().getMemberName());
    return result;
  }

  private static Map<String, Object> sdkCatalog(ObjectNode sdk) {
    Map<String, Object> result = new LinkedHashMap<>();
    result.put("module", stringMember(sdk, "module").orElse(""));
    result.put("method", stringMember(sdk, "method").orElse(""));
    result.put("paginated", booleanMember(sdk, "paginated").orElse(false));
    result.put("retryable", booleanMember(sdk, "retryable").orElse(false));
    return result;
  }

  private static List<Map<String, Object>> errorCatalogs(Model model, OperationShape operation) {
    List<Map<String, Object>> result = new ArrayList<>();
    for (ShapeId errorId : operation.getErrorsSet().stream().sorted().toList()) {
      Shape error = model.expectShape(errorId);
      ObjectNode problem = objectTrait(error, PROBLEM).orElseThrow();

      Map<String, Object> errorNode = new LinkedHashMap<>();
      errorNode.put("shape_id", errorId.toString());
      errorNode.put("type", stringMember(problem, "type").orElse(""));
      errorNode.put("code", stringMember(problem, "code").orElse(""));
      errorNode.put("status", numberTrait(error, HTTP_ERROR).orElse(0L));
      result.add(errorNode);
    }
    return result;
  }

  private static String permissionName(Model model, ShapeId permissionId) {
    return model
        .getShape(permissionId)
        .flatMap(shape -> objectTrait(shape, PERMISSION))
        .flatMap(node -> stringMember(node, "name"))
        .orElse(permissionId.toString());
  }

  private static String auditEventName(Model model, ShapeId eventId) {
    return model
        .getShape(eventId)
        .flatMap(shape -> objectTrait(shape, AUDIT_EVENT))
        .flatMap(node -> stringMember(node, "name"))
        .orElse(eventId.toString());
  }

  private static boolean isVerselfService(ServiceShape service) {
    String namespace = service.getId().getNamespace();
    return namespace.startsWith(VERSELF_NAMESPACE_PREFIX) && !namespace.equals(COMMON_NAMESPACE);
  }

  private static String operationEffect(OperationShape operation) {
    if (operation.hasTrait(READONLY)) {
      return "read";
    }
    return objectTrait(operation, OPERATION_SEMANTICS)
        .flatMap(node -> stringMember(node, "effect"))
        .orElse("write");
  }

  private static Optional<ObjectNode> objectTrait(Shape shape, ShapeId traitId) {
    return shape.findTrait(traitId).map(Trait::toNode).flatMap(Node::asObjectNode);
  }

  private static Optional<String> stringTrait(Shape shape, ShapeId traitId) {
    return shape
        .findTrait(traitId)
        .map(Trait::toNode)
        .flatMap(Node::asStringNode)
        .map(StringNode::getValue);
  }

  private static Optional<Long> numberTrait(Shape shape, ShapeId traitId) {
    return shape
        .findTrait(traitId)
        .map(Trait::toNode)
        .flatMap(Node::asNumberNode)
        .map(number -> number.getValue().longValue());
  }

  private static Optional<ShapeId> shapeIdMember(ObjectNode node, String member) {
    return node.getStringMember(member).map(StringNode::expectShapeId);
  }

  private static Optional<String> stringMember(ObjectNode node, String member) {
    return node.getStringMember(member).map(StringNode::getValue);
  }

  private static Optional<Boolean> booleanMember(ObjectNode node, String member) {
    return node.getBooleanMember(member).map(booleanNode -> booleanNode.getValue());
  }

  private static Optional<Long> longMember(ObjectNode node, String member) {
    return node.getNumberMember(member).map(number -> number.getValue().longValue());
  }

  private static List<String> stringListMember(ObjectNode node, String member) {
    return node.getArrayMember(member).map(VerselfRouteCatalogPlugin::stringList).orElse(List.of());
  }

  private static List<String> stringList(ArrayNode node) {
    List<String> values = new ArrayList<>();
    for (Node value : node.getElements()) {
      value.asStringNode().ifPresent(string -> values.add(string.getValue()));
    }
    return values;
  }

  private static <T extends Shape> List<T> sorted(Iterable<T> shapes) {
    List<T> result = new ArrayList<>();
    shapes.forEach(result::add);
    result.sort(Comparator.comparing(shape -> shape.getId().toString()));
    return result;
  }

  private static String kebabCase(String value) {
    return separatedCase(value, '-');
  }

  private static String snakeCase(String value) {
    return separatedCase(value, '_');
  }

  private static String separatedCase(String value, char separator) {
    StringBuilder out = new StringBuilder();
    for (int i = 0; i < value.length(); i++) {
      char c = value.charAt(i);
      if (Character.isUpperCase(c)) {
        if (i > 0 && startsNewWord(value, i)) {
          out.append(separator);
        }
        out.append(Character.toLowerCase(c));
      } else {
        out.append(c);
      }
    }
    return out.toString();
  }

  private static boolean startsNewWord(String value, int index) {
    char previous = value.charAt(index - 1);
    if (Character.isLowerCase(previous) || Character.isDigit(previous)) {
      return true;
    }
    int next = index + 1;
    return next < value.length() && Character.isLowerCase(value.charAt(next));
  }

  private static String toJson(Object value) {
    StringBuilder out = new StringBuilder();
    appendJson(out, value);
    return out.toString();
  }

  private static void appendJson(StringBuilder out, Object value) {
    if (value == null) {
      out.append("null");
    } else if (value instanceof String) {
      appendJsonString(out, (String) value);
    } else if (value instanceof Number || value instanceof Boolean) {
      out.append(value);
    } else if (value instanceof Map) {
      appendJsonObject(out, (Map<?, ?>) value);
    } else if (value instanceof Iterable) {
      appendJsonArray(out, (Iterable<?>) value);
    } else {
      appendJsonString(out, value.toString());
    }
  }

  private static void appendJsonObject(StringBuilder out, Map<?, ?> map) {
    out.append('{');
    boolean first = true;
    for (Map.Entry<?, ?> entry : map.entrySet()) {
      if (!first) {
        out.append(',');
      }
      first = false;
      appendJsonString(out, entry.getKey().toString());
      out.append(':');
      appendJson(out, entry.getValue());
    }
    out.append('}');
  }

  private static void appendJsonArray(StringBuilder out, Iterable<?> values) {
    out.append('[');
    boolean first = true;
    for (Object value : values) {
      if (!first) {
        out.append(',');
      }
      first = false;
      appendJson(out, value);
    }
    out.append(']');
  }

  private static void appendJsonString(StringBuilder out, String value) {
    out.append('"');
    for (int i = 0; i < value.length(); i++) {
      char c = value.charAt(i);
      switch (c) {
        case '"':
          out.append("\\\"");
          break;
        case '\\':
          out.append("\\\\");
          break;
        case '\b':
          out.append("\\b");
          break;
        case '\f':
          out.append("\\f");
          break;
        case '\n':
          out.append("\\n");
          break;
        case '\r':
          out.append("\\r");
          break;
        case '\t':
          out.append("\\t");
          break;
        default:
          if (c < 0x20) {
            out.append(String.format("\\u%04x", (int) c));
          } else {
            out.append(c);
          }
      }
    }
    out.append('"');
  }
}
