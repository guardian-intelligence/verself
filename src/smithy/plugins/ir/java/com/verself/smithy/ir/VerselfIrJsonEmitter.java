package com.verself.smithy.ir;

import static com.verself.smithy.ir.SmithyDiagnostics.missing;
import static com.verself.smithy.ir.SmithyNames.localName;
import static com.verself.smithy.ir.SmithyNames.lowerKebab;
import static com.verself.smithy.ir.SmithyNames.lowerSnake;
import static com.verself.smithy.ir.SmithyNodes.numberMember;
import static com.verself.smithy.ir.SmithyNodes.objectTrait;
import static com.verself.smithy.ir.SmithyNodes.requiredArray;
import static com.verself.smithy.ir.SmithyNodes.requiredObjectTrait;
import static com.verself.smithy.ir.SmithyNodes.requiredString;
import static com.verself.smithy.ir.SmithyNodes.stringTrait;
import static com.verself.smithy.ir.VerselfTraitIds.AUDIT;
import static com.verself.smithy.ir.VerselfTraitIds.AUDIT_EVENT;
import static com.verself.smithy.ir.VerselfTraitIds.AUTHZ;
import static com.verself.smithy.ir.VerselfTraitIds.ENUM_VALUE;
import static com.verself.smithy.ir.VerselfTraitIds.HTTP;
import static com.verself.smithy.ir.VerselfTraitIds.HTTP_ERROR;
import static com.verself.smithy.ir.VerselfTraitIds.HTTP_HEADER;
import static com.verself.smithy.ir.VerselfTraitIds.HTTP_LABEL;
import static com.verself.smithy.ir.VerselfTraitIds.HTTP_PAYLOAD;
import static com.verself.smithy.ir.VerselfTraitIds.HTTP_QUERY;
import static com.verself.smithy.ir.VerselfTraitIds.IDEMPOTENCY_TOKEN;
import static com.verself.smithy.ir.VerselfTraitIds.IDEMPOTENT;
import static com.verself.smithy.ir.VerselfTraitIds.IDENTITY;
import static com.verself.smithy.ir.VerselfTraitIds.INPUT;
import static com.verself.smithy.ir.VerselfTraitIds.LENGTH;
import static com.verself.smithy.ir.VerselfTraitIds.MEDIA_TYPE;
import static com.verself.smithy.ir.VerselfTraitIds.NESTED_PROPERTIES;
import static com.verself.smithy.ir.VerselfTraitIds.NOT_RESOURCE_PROPERTY;
import static com.verself.smithy.ir.VerselfTraitIds.OUTPUT;
import static com.verself.smithy.ir.VerselfTraitIds.PAGINATED;
import static com.verself.smithy.ir.VerselfTraitIds.PATTERN;
import static com.verself.smithy.ir.VerselfTraitIds.PERMISSION;
import static com.verself.smithy.ir.VerselfTraitIds.PROBLEM;
import static com.verself.smithy.ir.VerselfTraitIds.PROTO_FIELD;
import static com.verself.smithy.ir.VerselfTraitIds.RANGE;
import static com.verself.smithy.ir.VerselfTraitIds.RATE_LIMIT;
import static com.verself.smithy.ir.VerselfTraitIds.READONLY;
import static com.verself.smithy.ir.VerselfTraitIds.REQUEST_BUDGET;
import static com.verself.smithy.ir.VerselfTraitIds.REQUIRED;
import static com.verself.smithy.ir.VerselfTraitIds.SDK;
import static com.verself.smithy.ir.VerselfTraitIds.SENSITIVE;
import static com.verself.smithy.ir.VerselfTraitIds.SERVICE_RUNTIME;
import static com.verself.smithy.ir.VerselfTraitIds.STREAMING;

import java.util.ArrayList;
import java.util.Comparator;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.stream.Collectors;
import software.amazon.smithy.build.SmithyBuildException;
import software.amazon.smithy.model.Model;
import software.amazon.smithy.model.SourceLocation;
import software.amazon.smithy.model.node.Node;
import software.amazon.smithy.model.node.ObjectNode;
import software.amazon.smithy.model.shapes.ListShape;
import software.amazon.smithy.model.shapes.MemberShape;
import software.amazon.smithy.model.shapes.OperationShape;
import software.amazon.smithy.model.shapes.ResourceShape;
import software.amazon.smithy.model.shapes.ServiceShape;
import software.amazon.smithy.model.shapes.Shape;
import software.amazon.smithy.model.shapes.ShapeId;
import software.amazon.smithy.model.shapes.StructureShape;
import software.amazon.smithy.model.traits.Trait;

final class VerselfIrJsonEmitter {
  private static final String IR_VERSION = "verself.contract-ir.v1";

  private VerselfIrJsonEmitter() {}

  static ObjectNode toNode(VerselfIrModel contract) {
    SmithyModelIndex index = contract.index();
    return Node.objectNodeBuilder()
        .withMember("irVersion", IR_VERSION)
        .withMember("package", contract.packageName())
        .withMember("projection", contract.projection())
        .withMember("visibility", contract.visibility())
        .withMember("service", serviceNode(index.service()))
        .withMember("shapes", shapeMapNode(index.model(), index.shapes()))
        .withMember("operations", operationArrayNode(index.model(), index.operations()))
        .withMember("resources", resourceArrayNode(index.resources()))
        .withMember("problems", problemArrayNode(index.model(), index.operations()))
        .withMember("catalogs", catalogsNode(index.model(), index.service(), index.operations()))
        .withMember(
            "source",
            sourceMapNode(index.service(), index.shapes(), index.operations(), index.resources()))
        .build();
  }

  private static ObjectNode serviceNode(ServiceShape service) {
    ObjectNode.Builder builder =
        Node.objectNodeBuilder()
            .withMember("shapeId", service.getId().toString())
            .withMember("namespace", service.getId().getNamespace())
            .withMember("name", service.getId().getName())
            .withMember("version", service.getVersion());

    objectTrait(service, SERVICE_RUNTIME)
        .ifPresent(runtime -> builder.withMember("runtime", runtime));
    return builder.build();
  }

  private static ObjectNode shapeMapNode(Model model, List<Shape> shapes) {
    ObjectNode.Builder builder = Node.objectNodeBuilder();
    for (Shape shape : shapes) {
      builder.withMember(shape.getId().toString(), shapeNode(model, shape));
    }
    return builder.build();
  }

  private static ObjectNode shapeNode(Model model, Shape shape) {
    ObjectNode.Builder builder =
        Node.objectNodeBuilder()
            .withMember("kind", shape.getType().toString())
            .withMember("name", shape.getId().getName());

    ObjectNode constraints = constraintsNode(shape);
    if (!constraints.isEmpty()) {
      builder.withMember("constraints", constraints);
    }
    String mediaType = stringTrait(shape, MEDIA_TYPE);
    if (!mediaType.isEmpty()) {
      builder.withMember("mediaType", mediaType);
    }
    if (shape.hasTrait(STREAMING)) {
      builder.withMember("streaming", true);
    }
    if (shape.hasTrait(SENSITIVE)) {
      builder.withMember("sensitive", true);
    }
    if (shape.hasTrait(INPUT)) {
      builder.withMember("input", true);
    }
    if (shape.hasTrait(OUTPUT)) {
      builder.withMember("output", true);
    }
    if (shape.isStructureShape()) {
      builder.withMember("members", memberArrayNode(model, shape.asStructureShape().get()));
    }
    if (shape.isListShape()) {
      ListShape list = shape.asListShape().get();
      builder.withMember("member", memberTargetNode(list.getMember()));
    }
    if (shape.isEnumShape()) {
      builder.withMember("enum", enumArrayNode(shape));
    }
    return builder.build();
  }

  private static Node memberTargetNode(MemberShape member) {
    return Node.objectNodeBuilder().withMember("target", member.getTarget().toString()).build();
  }

  private static Node memberArrayNode(Model model, StructureShape structure) {
    List<MemberShape> members = new ArrayList<>(structure.getAllMembers().values());
    members.sort(
        Comparator.comparingInt(VerselfIrJsonEmitter::protoFieldNumber)
            .thenComparing(MemberShape::getMemberName));
    List<Node> nodes = new ArrayList<>();
    for (MemberShape member : members) {
      nodes.add(memberNode(model, member));
    }
    return Node.fromNodes(nodes);
  }

  private static ObjectNode memberNode(Model model, MemberShape member) {
    ObjectNode.Builder builder =
        Node.objectNodeBuilder()
            .withMember("name", member.getMemberName())
            .withMember("target", member.getTarget().toString())
            .withMember("jsonName", member.getMemberName())
            .withMember("required", member.hasTrait(REQUIRED));

    httpBindingNode(member).ifPresent(binding -> builder.withMember("httpBinding", binding));
    objectTrait(member, PROTO_FIELD).ifPresent(proto -> builder.withMember("protoField", proto));
    if (member.hasTrait(NESTED_PROPERTIES)) {
      builder.withMember("nestedProperties", true);
    }
    if (member.hasTrait(IDEMPOTENCY_TOKEN)) {
      builder.withMember("idempotencyToken", true);
    }
    if (member.hasTrait(NOT_RESOURCE_PROPERTY)) {
      builder.withMember("notResourceProperty", true);
    }
    if (member.hasTrait(SENSITIVE)
        || model
            .getShape(member.getTarget())
            .map(shape -> shape.hasTrait(SENSITIVE))
            .orElse(false)) {
      builder.withMember("sensitive", true);
    }
    return builder.build();
  }

  private static Optional<ObjectNode> httpBindingNode(MemberShape member) {
    if (member.hasTrait(HTTP_LABEL)) {
      return Optional.of(
          Node.objectNodeBuilder()
              .withMember("location", "label")
              .withMember("name", member.getMemberName())
              .build());
    }
    if (member.hasTrait(HTTP_PAYLOAD)) {
      return Optional.of(
          Node.objectNodeBuilder()
              .withMember("location", "payload")
              .withMember("name", member.getMemberName())
              .build());
    }
    String header = stringTrait(member, HTTP_HEADER);
    if (!header.isEmpty()) {
      return Optional.of(
          Node.objectNodeBuilder()
              .withMember("location", "header")
              .withMember("name", header)
              .build());
    }
    String query = stringTrait(member, HTTP_QUERY);
    if (!query.isEmpty()) {
      return Optional.of(
          Node.objectNodeBuilder()
              .withMember("location", "query")
              .withMember("name", query)
              .build());
    }
    return Optional.of(
        Node.objectNodeBuilder()
            .withMember("location", "document")
            .withMember("name", member.getMemberName())
            .build());
  }

  private static Node enumArrayNode(Shape shape) {
    List<MemberShape> members = new ArrayList<>(shape.members());
    members.sort(Comparator.comparing(MemberShape::getMemberName));
    List<Node> nodes = new ArrayList<>();
    for (MemberShape member : members) {
      String value = stringTrait(member, ENUM_VALUE);
      if (value.isEmpty()) {
        value = member.getMemberName();
      }
      nodes.add(
          Node.objectNodeBuilder()
              .withMember("name", member.getMemberName())
              .withMember("value", value)
              .build());
    }
    return Node.fromNodes(nodes);
  }

  private static Node operationArrayNode(Model model, List<OperationShape> operations) {
    List<Node> nodes = new ArrayList<>();
    for (OperationShape operation : operations) {
      nodes.add(operationNode(model, operation));
    }
    nodes.sort(
        Comparator.comparing(
            node -> node.expectObjectNode().expectStringMember("operationId").getValue()));
    return Node.fromNodes(nodes);
  }

  private static ObjectNode operationNode(Model model, OperationShape operation) {
    ObjectNode http = requiredObjectTrait(operation, HTTP);
    ShapeId input = operation.getInput().orElseThrow(() -> missing(operation, "input target"));
    ShapeId output = operation.getOutput().orElseThrow(() -> missing(operation, "output target"));
    List<ShapeId> errors = operation.getErrorsSet().stream().sorted().collect(Collectors.toList());

    return Node.objectNodeBuilder()
        .withMember("shapeId", operation.getId().toString())
        .withMember("name", operation.getId().getName())
        .withMember("operationId", lowerKebab(operation.getId().getName()))
        .withMember("readonly", operation.hasTrait(READONLY))
        .withMember("idempotent", operation.hasTrait(IDEMPOTENT))
        .withMember("paginated", operation.hasTrait(PAGINATED))
        .withMember(
            "http",
            Node.objectNodeBuilder()
                .withMember("method", requiredString(http, "method", operation))
                .withMember("path", requiredString(http, "uri", operation))
                .withMember("successStatus", numberMember(http, "code").orElse(200))
                .build())
        .withMember("input", input.toString())
        .withMember("output", output.toString())
        .withMember(
            "errors",
            Node.fromStrings(errors.stream().map(ShapeId::toString).collect(Collectors.toList())))
        .withMember("bindings", bindingsNode(model, input, output))
        .withMember("verself", verselfNode(model, operation, input))
        .withMember("problems", problemsForErrors(model, errors))
        .build();
  }

  private static ObjectNode bindingsNode(Model model, ShapeId inputId, ShapeId outputId) {
    StructureShape input = structureShape(model, inputId, "input");
    StructureShape output = structureShape(model, outputId, "output");
    List<Node> labels = new ArrayList<>();
    List<Node> headers = new ArrayList<>();
    List<Node> queries = new ArrayList<>();
    List<String> documentMembers = new ArrayList<>();
    Optional<ObjectNode> payload = Optional.empty();
    for (MemberShape member : orderedMembers(input)) {
      if (member.hasTrait(HTTP_LABEL)) {
        labels.add(bindingMemberNode(member, member.getMemberName()));
      } else if (member.hasTrait(HTTP_PAYLOAD)) {
        if (payload.isPresent()) {
          throw new SmithyBuildException(inputId + " has multiple @httpPayload members");
        }
        payload = Optional.of(payloadBindingNode(model, member));
      } else if (!stringTrait(member, HTTP_HEADER).isEmpty()) {
        headers.add(bindingMemberNode(member, stringTrait(member, HTTP_HEADER)));
      } else if (!stringTrait(member, HTTP_QUERY).isEmpty()) {
        queries.add(bindingMemberNode(member, stringTrait(member, HTTP_QUERY)));
      } else {
        documentMembers.add(member.getMemberName());
      }
    }
    if (payload.isPresent() && !documentMembers.isEmpty()) {
      throw new SmithyBuildException(inputId + " cannot mix @httpPayload and document members");
    }

    List<Node> responseHeaders = new ArrayList<>();
    List<String> responseDocumentMembers = new ArrayList<>();
    Optional<ObjectNode> responsePayload = Optional.empty();
    for (MemberShape member : orderedMembers(output)) {
      if (member.hasTrait(HTTP_PAYLOAD)) {
        if (responsePayload.isPresent()) {
          throw new SmithyBuildException(outputId + " has multiple @httpPayload members");
        }
        responsePayload = Optional.of(payloadBindingNode(model, member));
      } else if (!stringTrait(member, HTTP_HEADER).isEmpty()) {
        responseHeaders.add(bindingMemberNode(member, stringTrait(member, HTTP_HEADER)));
      } else {
        responseDocumentMembers.add(member.getMemberName());
      }
    }
    if (responsePayload.isPresent() && !responseDocumentMembers.isEmpty()) {
      throw new SmithyBuildException(outputId + " cannot mix @httpPayload and document members");
    }

    documentMembers.sort(String::compareTo);
    responseDocumentMembers.sort(String::compareTo);
    ObjectNode.Builder builder =
        Node.objectNodeBuilder()
            .withMember("labels", Node.fromNodes(labels))
            .withMember("headers", Node.fromNodes(headers))
            .withMember("queries", Node.fromNodes(queries))
            .withMember("documentMembers", Node.fromStrings(documentMembers))
            .withMember("responseHeaders", Node.fromNodes(responseHeaders))
            .withMember("responseDocumentMembers", Node.fromStrings(responseDocumentMembers));
    payload.ifPresent(value -> builder.withMember("payload", value));
    responsePayload.ifPresent(value -> builder.withMember("responsePayload", value));
    return builder.build();
  }

  private static StructureShape structureShape(Model model, ShapeId id, String role) {
    Shape shape =
        model
            .getShape(id)
            .orElseThrow(() -> new SmithyBuildException("missing " + role + " " + id));
    return shape
        .asStructureShape()
        .orElseThrow(() -> new SmithyBuildException(id + " is not a structure " + role));
  }

  private static ObjectNode bindingMemberNode(MemberShape member, String wireName) {
    return Node.objectNodeBuilder()
        .withMember("member", member.getMemberName())
        .withMember("name", wireName)
        .build();
  }

  private static ObjectNode payloadBindingNode(Model model, MemberShape member) {
    Optional<Shape> target = model.getShape(member.getTarget());
    String kind =
        target
            .map(shape -> shape.getType().toString())
            .orElseGet(() -> localName(member.getTarget().toString()).toLowerCase());
    String mediaType = target.map(shape -> stringTrait(shape, MEDIA_TYPE)).orElse("");
    boolean streaming = target.map(shape -> shape.hasTrait(STREAMING)).orElse(false);
    boolean sensitive =
        member.hasTrait(SENSITIVE) || target.map(shape -> shape.hasTrait(SENSITIVE)).orElse(false);
    return Node.objectNodeBuilder()
        .withMember("member", member.getMemberName())
        .withMember("target", member.getTarget().toString())
        .withMember("kind", kind)
        .withMember("mediaType", mediaType)
        .withMember("streaming", streaming)
        .withMember("sensitive", sensitive)
        .withMember("required", member.hasTrait(REQUIRED))
        .build();
  }

  private static ObjectNode verselfNode(Model model, OperationShape operation, ShapeId inputId) {
    ObjectNode identity = requiredObjectTrait(operation, IDENTITY);
    ObjectNode authz = requiredObjectTrait(operation, AUTHZ);
    ObjectNode audit = requiredObjectTrait(operation, AUDIT);
    ObjectNode rateLimit = requiredObjectTrait(operation, RATE_LIMIT);
    ObjectNode requestBudget = requiredObjectTrait(operation, REQUEST_BUDGET);
    ObjectNode sdk = requiredObjectTrait(operation, SDK);

    String permissionShapeId = requiredString(authz, "permission", operation);
    String auditEventShapeId = requiredString(audit, "event", operation);
    String auditResourceShapeId = requiredString(audit, "resource", operation);
    ObjectNode organization = authz.expectObjectMember("organization");
    Optional<ObjectNode> idempotency = idempotencyNode(model, inputId);

    ObjectNode.Builder builder =
        Node.objectNodeBuilder()
            .withMember(
                "identity",
                Node.objectNodeBuilder()
                    .withMember("mode", requiredString(identity, "mode", operation))
                    .withMember("audience", requiredString(identity, "audience", operation))
                    .withMember("principals", requiredArray(identity, "principals", operation))
                    .build())
            .withMember(
                "authz",
                Node.objectNodeBuilder()
                    .withMember("permission", permissionName(model, permissionShapeId, operation))
                    .withMember("permissionShape", permissionShapeId)
                    .withMember("organization", organization)
                    .build())
            .withMember(
                "audit",
                Node.objectNodeBuilder()
                    .withMember("event", auditEventName(model, auditEventShapeId, operation))
                    .withMember("eventShape", auditEventShapeId)
                    .withMember("resource", lowerSnake(localName(auditResourceShapeId)))
                    .withMember("resourceShape", auditResourceShapeId)
                    .withMember("action", requiredString(audit, "action", operation))
                    .build())
            .withMember("rateLimit", rateLimit)
            .withMember("requestBudget", requestBudget)
            .withMember("sdk", sdk);
    idempotency.ifPresent(value -> builder.withMember("idempotency", value));
    return builder.build();
  }

  private static Optional<ObjectNode> idempotencyNode(Model model, ShapeId inputId) {
    Shape shape =
        model
            .getShape(inputId)
            .orElseThrow(() -> new SmithyBuildException("missing input " + inputId));
    StructureShape input =
        shape
            .asStructureShape()
            .orElseThrow(() -> new SmithyBuildException(inputId + " is not a structure input"));
    for (MemberShape member : input.getAllMembers().values()) {
      if (!member.hasTrait(IDEMPOTENCY_TOKEN)) {
        continue;
      }
      String header = stringTrait(member, HTTP_HEADER);
      if (!header.isEmpty()) {
        return Optional.of(
            Node.objectNodeBuilder()
                .withMember("policy", "idempotency_key_header")
                .withMember("header", header)
                .withMember("member", member.getMemberName())
                .build());
      }
      if (member.hasTrait(HTTP_LABEL) || !stringTrait(member, HTTP_QUERY).isEmpty()) {
        throw new SmithyBuildException(
            member.getId() + " has @idempotencyToken outside request document or header");
      }
      return Optional.of(
          Node.objectNodeBuilder()
              .withMember("policy", "request_body_idempotency_key")
              .withMember("member", member.getMemberName())
              .build());
    }
    return Optional.empty();
  }

  private static Node resourceArrayNode(List<ResourceShape> resources) {
    List<Node> nodes = new ArrayList<>();
    for (ResourceShape resource : resources) {
      nodes.add(resourceNode(resource));
    }
    return Node.fromNodes(nodes);
  }

  private static ObjectNode resourceNode(ResourceShape resource) {
    ObjectNode.Builder identifiers = Node.objectNodeBuilder();
    resource.getIdentifiers().entrySet().stream()
        .sorted(Map.Entry.comparingByKey())
        .forEach(entry -> identifiers.withMember(entry.getKey(), entry.getValue().toString()));
    ObjectNode.Builder properties = Node.objectNodeBuilder();
    resource.getProperties().entrySet().stream()
        .sorted(Map.Entry.comparingByKey())
        .forEach(entry -> properties.withMember(entry.getKey(), entry.getValue().toString()));
    return Node.objectNodeBuilder()
        .withMember("shapeId", resource.getId().toString())
        .withMember("name", resource.getId().getName())
        .withMember("identifiers", identifiers.build())
        .withMember("properties", properties.build())
        .build();
  }

  private static Node problemArrayNode(Model model, List<OperationShape> operations) {
    Map<String, Node> problems = new LinkedHashMap<>();
    for (OperationShape operation : operations) {
      for (ShapeId error : operation.getErrorsSet()) {
        problems.put(error.toString(), problemNode(model, error));
      }
    }
    return Node.fromNodes(
        problems.values().stream()
            .sorted(
                Comparator.comparing(
                    node -> node.expectObjectNode().expectStringMember("shapeId").getValue()))
            .collect(Collectors.toList()));
  }

  private static Node problemsForErrors(Model model, List<ShapeId> errors) {
    List<Node> nodes = new ArrayList<>();
    for (ShapeId error : errors) {
      nodes.add(problemNode(model, error));
    }
    return Node.fromNodes(nodes);
  }

  private static ObjectNode problemNode(Model model, ShapeId errorId) {
    Shape shape =
        model
            .getShape(errorId)
            .orElseThrow(() -> new SmithyBuildException("missing error " + errorId));
    ObjectNode problem = requiredObjectTrait(shape, PROBLEM);
    return Node.objectNodeBuilder()
        .withMember("shapeId", errorId.toString())
        .withMember("name", errorId.getName())
        .withMember("status", httpErrorStatus(shape))
        .withMember("type", requiredString(problem, "type", shape))
        .withMember("code", requiredString(problem, "code", shape))
        .build();
  }

  private static ObjectNode catalogsNode(
      Model model, ServiceShape service, List<OperationShape> operations) {
    String serviceName =
        requiredString(requiredObjectTrait(service, SERVICE_RUNTIME), "serviceName", service);
    List<Node> iam = new ArrayList<>();
    List<Node> audit = new ArrayList<>();
    List<Node> observability = new ArrayList<>();
    for (OperationShape operation : operations) {
      ObjectNode verself =
          verselfNode(
              model,
              operation,
              operation.getInput().orElseThrow(() -> missing(operation, "input target")));
      ObjectNode authz = verself.expectObjectMember("authz");
      ObjectNode auditNode = verself.expectObjectMember("audit");
      ObjectNode organization = authz.expectObjectMember("organization");
      ObjectNode http = requiredObjectTrait(operation, HTTP);
      String operationId = lowerKebab(operation.getId().getName());
      String orgScope = runtimeOrgScope(requiredString(organization, "source", operation));

      iam.add(
          Node.objectNodeBuilder()
              .withMember("operationId", operationId)
              .withMember("permission", requiredString(authz, "permission", operation))
              .withMember("resource", requiredString(auditNode, "resource", operation))
              .withMember("action", requiredString(auditNode, "action", operation))
              .withMember("orgScope", orgScope)
              .withMember(
                  "memberEligible", memberEligible(requiredString(auditNode, "action", operation)))
              .build());
      audit.add(
          Node.objectNodeBuilder()
              .withMember("operationId", operationId)
              .withMember("event", requiredString(auditNode, "event", operation))
              .withMember("resource", requiredString(auditNode, "resource", operation))
              .withMember("resourceShape", requiredString(auditNode, "resourceShape", operation))
              .withMember("action", requiredString(auditNode, "action", operation))
              .withMember("orgScope", orgScope)
              .build());
      observability.add(
          Node.objectNodeBuilder()
              .withMember("operationId", operationId)
              .withMember("service", serviceName)
              .withMember("httpMethod", requiredString(http, "method", operation))
              .withMember("httpRoute", requiredString(http, "uri", operation))
              .withMember("permission", requiredString(authz, "permission", operation))
              .withMember("auditEvent", requiredString(auditNode, "event", operation))
              .withMember(
                  "rateLimitBucket",
                  requiredString(verself.expectObjectMember("rateLimit"), "bucket", operation))
              .withMember(
                  "bodyBudgetBytes",
                  numberMember(verself.expectObjectMember("requestBudget"), "maxBytes").orElse(0))
              .build());
    }
    return Node.objectNodeBuilder()
        .withMember("iam", Node.fromNodes(iam))
        .withMember("audit", Node.fromNodes(audit))
        .withMember("observability", Node.fromNodes(observability))
        .build();
  }

  private static ObjectNode sourceMapNode(
      ServiceShape service,
      List<Shape> shapes,
      List<OperationShape> operations,
      List<ResourceShape> resources) {
    ObjectNode.Builder builder =
        Node.objectNodeBuilder()
            .withMember(service.getId().toString(), sourceNode(service.getSourceLocation()));
    for (Shape shape : shapes) {
      builder.withMember(shape.getId().toString(), sourceNode(shape.getSourceLocation()));
    }
    for (OperationShape operation : operations) {
      builder.withMember(operation.getId().toString(), sourceNode(operation.getSourceLocation()));
    }
    for (ResourceShape resource : resources) {
      builder.withMember(resource.getId().toString(), sourceNode(resource.getSourceLocation()));
    }
    return builder.build();
  }

  private static ObjectNode sourceNode(SourceLocation source) {
    return Node.objectNodeBuilder()
        .withMember("filename", source.getFilename())
        .withMember("line", source.getLine())
        .withMember("column", source.getColumn())
        .build();
  }

  private static List<MemberShape> orderedMembers(StructureShape structure) {
    List<MemberShape> members = new ArrayList<>(structure.getAllMembers().values());
    members.sort(Comparator.comparing(MemberShape::getMemberName));
    return members;
  }

  private static ObjectNode constraintsNode(Shape shape) {
    ObjectNode.Builder builder = Node.objectNodeBuilder();
    objectTrait(shape, LENGTH).ifPresent(length -> builder.withMember("length", length));
    objectTrait(shape, RANGE).ifPresent(range -> builder.withMember("range", range));
    String pattern = stringTrait(shape, PATTERN);
    if (!pattern.isEmpty()) {
      builder.withMember("pattern", pattern);
    }
    return builder.build();
  }

  private static int protoFieldNumber(MemberShape member) {
    return objectTrait(member, PROTO_FIELD)
        .flatMap(node -> numberMember(node, "number"))
        .orElse(1_000_000);
  }

  private static int httpErrorStatus(Shape shape) {
    return shape
        .findTrait(ShapeId.from(HTTP_ERROR))
        .map(Trait::toNode)
        .flatMap(Node::asNumberNode)
        .map(number -> number.getValue().intValue())
        .orElse(500);
  }

  private static String permissionName(Model model, String permissionShapeId, Shape owner) {
    Shape permissionShape =
        model
            .getShape(ShapeId.from(permissionShapeId))
            .orElseThrow(
                () ->
                    new SmithyBuildException(
                        owner.getId() + " references missing permission " + permissionShapeId));
    return requiredString(
        requiredObjectTrait(permissionShape, PERMISSION), "name", permissionShape);
  }

  private static String auditEventName(Model model, String eventShapeId, Shape owner) {
    Shape eventShape =
        model
            .getShape(ShapeId.from(eventShapeId))
            .orElseThrow(
                () ->
                    new SmithyBuildException(
                        owner.getId() + " references missing audit event " + eventShapeId));
    return requiredString(requiredObjectTrait(eventShape, AUDIT_EVENT), "name", eventShape);
  }

  private static boolean memberEligible(String action) {
    return action.equals("read") || action.equals("list");
  }

  private static String runtimeOrgScope(String source) {
    switch (source) {
      case "token_org_id":
        return "token_org_id";
      case "token_role_assignments":
        return "token_role_assignment_org_ids";
      case "input_member":
        return "path_org_id";
      case "request_subject":
        return "token_subject";
      case "checkout_grant":
        return "checkout_grant";
      default:
        return source;
    }
  }
}
