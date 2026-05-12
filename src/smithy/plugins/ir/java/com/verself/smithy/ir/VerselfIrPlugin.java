package com.verself.smithy.ir;

import java.util.ArrayList;
import java.util.Collection;
import java.util.Comparator;
import java.util.LinkedHashMap;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.Set;
import java.util.regex.Pattern;
import java.util.stream.Collectors;
import software.amazon.smithy.build.PluginContext;
import software.amazon.smithy.build.SmithyBuildException;
import software.amazon.smithy.build.SmithyBuildPlugin;
import software.amazon.smithy.model.Model;
import software.amazon.smithy.model.SourceLocation;
import software.amazon.smithy.model.knowledge.TopDownIndex;
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

/**
 * Emits the Verself contract IR for a projected Smithy model.
 */
public final class VerselfIrPlugin implements SmithyBuildPlugin {
    private static final String IR_VERSION = "verself.contract-ir.v1";

    private static final ShapeId AUDIT = ShapeId.from("verself.common.v1#audit");
    private static final ShapeId AUDIT_EVENT = ShapeId.from("verself.common.v1#auditEvent");
    private static final ShapeId AUTHZ = ShapeId.from("verself.common.v1#authz");
    private static final ShapeId CONFORMANCE = ShapeId.from("verself.common.v1#conformance");
    private static final ShapeId IDENTITY = ShapeId.from("verself.common.v1#identity");
    private static final ShapeId NOT_RESOURCE_PROPERTY = ShapeId.from("verself.common.v1#notResourceProperty");
    private static final ShapeId PERMISSION = ShapeId.from("verself.common.v1#permission");
    private static final ShapeId PROBLEM = ShapeId.from("verself.common.v1#problem");
    private static final ShapeId PROTO_FIELD = ShapeId.from("verself.common.v1#protoField");
    private static final ShapeId RATE_LIMIT = ShapeId.from("verself.common.v1#rateLimit");
    private static final ShapeId REQUEST_BUDGET = ShapeId.from("verself.common.v1#requestBudget");
    private static final ShapeId SDK = ShapeId.from("verself.common.v1#sdk");
    private static final ShapeId SERVICE_RUNTIME = ShapeId.from("verself.common.v1#serviceRuntime");

    private static final String ENUM_VALUE = "smithy.api#enumValue";
    private static final String HTTP = "smithy.api#http";
    private static final String HTTP_ERROR = "smithy.api#httpError";
    private static final String HTTP_HEADER = "smithy.api#httpHeader";
    private static final String HTTP_LABEL = "smithy.api#httpLabel";
    private static final String HTTP_QUERY = "smithy.api#httpQuery";
    private static final String IDEMPOTENCY_TOKEN = "smithy.api#idempotencyToken";
    private static final String IDEMPOTENT = "smithy.api#idempotent";
    private static final String INPUT = "smithy.api#input";
    private static final String LENGTH = "smithy.api#length";
    private static final String NESTED_PROPERTIES = "smithy.api#nestedProperties";
    private static final String OUTPUT = "smithy.api#output";
    private static final String PAGINATED = "smithy.api#paginated";
    private static final String PATTERN = "smithy.api#pattern";
    private static final String RANGE = "smithy.api#range";
    private static final String READONLY = "smithy.api#readonly";
    private static final String REQUIRED = "smithy.api#required";
    private static final String SENSITIVE = "smithy.api#sensitive";

    private static final Pattern WORD_BOUNDARY = Pattern.compile("([a-z0-9])([A-Z])");

    @Override
    public String getName() {
        return "verself-ir";
    }

    @Override
    public void execute(PluginContext context) {
        Model model = context.getModel();
        ObjectNode settings = context.getSettings();
        ServiceShape service = resolveService(model, settings);
        TopDownIndex topDown = TopDownIndex.of(model);

        String packageName = stringSetting(settings, "package").orElse(service.getId().getNamespace());
        String projection = context.getProjectionName();
        String visibility = stringSetting(settings, "visibility").orElse(projection);
        String filename = stringSetting(settings, "filename")
                .orElse("ir/" + packageName + "/" + projection + ".json");

        List<OperationShape> operations = sortedShapes(topDown.getContainedOperations(service));
        List<ResourceShape> resources = sortedShapes(topDown.getContainedResources(service));
        Set<ShapeId> shapeClosure = shapeClosure(model, operations, resources);
        List<Shape> shapes = shapeClosure.stream()
                .map(id -> model.getShape(id).orElseThrow(() -> new SmithyBuildException("missing shape " + id)))
                .sorted(Comparator.comparing(shape -> shape.getId().toString()))
                .collect(Collectors.toList());

        ObjectNode ir = Node.objectNodeBuilder()
                .withMember("irVersion", IR_VERSION)
                .withMember("package", packageName)
                .withMember("projection", projection)
                .withMember("visibility", visibility)
                .withMember("service", serviceNode(service))
                .withMember("shapes", shapeMapNode(model, shapes))
                .withMember("operations", operationArrayNode(model, operations))
                .withMember("resources", resourceArrayNode(resources))
                .withMember("problems", problemArrayNode(model, operations))
                .withMember("catalogs", catalogsNode(model, service, operations))
                .withMember("source", sourceMapNode(service, shapes, operations, resources))
                .build();

        context.getFileManifest().writeJson(filename, ir);
    }

    private static ServiceShape resolveService(Model model, ObjectNode settings) {
        Optional<String> configured = stringSetting(settings, "service");
        if (configured.isPresent()) {
            ShapeId id = ShapeId.from(configured.get());
            return model.getShape(id)
                    .flatMap(Shape::asServiceShape)
                    .orElseThrow(() -> new SmithyBuildException("verself-ir service `" + id + "` was not found"));
        }

        List<ServiceShape> services = sortedShapes(model.getServiceShapes());
        if (services.size() != 1) {
            throw new SmithyBuildException(
                    "verself-ir requires a `service` setting when the projection contains "
                            + services.size()
                            + " services");
        }
        return services.get(0);
    }

    private static ObjectNode serviceNode(ServiceShape service) {
        ObjectNode.Builder builder = Node.objectNodeBuilder()
                .withMember("shapeId", service.getId().toString())
                .withMember("namespace", service.getId().getNamespace())
                .withMember("name", service.getId().getName())
                .withMember("version", service.getVersion());

        objectTrait(service, SERVICE_RUNTIME).ifPresent(runtime -> builder.withMember("runtime", runtime));
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
        ObjectNode.Builder builder = Node.objectNodeBuilder()
                .withMember("kind", shape.getType().toString())
                .withMember("name", shape.getId().getName());

        ObjectNode constraints = constraintsNode(shape);
        if (!constraints.isEmpty()) {
            builder.withMember("constraints", constraints);
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
        return Node.objectNodeBuilder()
                .withMember("target", member.getTarget().toString())
                .build();
    }

    private static Node memberArrayNode(Model model, StructureShape structure) {
        List<MemberShape> members = new ArrayList<>(structure.getAllMembers().values());
        members.sort(Comparator
                .comparingInt(VerselfIrPlugin::protoFieldNumber)
                .thenComparing(MemberShape::getMemberName));
        List<Node> nodes = new ArrayList<>();
        for (MemberShape member : members) {
            nodes.add(memberNode(model, member));
        }
        return Node.fromNodes(nodes);
    }

    private static ObjectNode memberNode(Model model, MemberShape member) {
        ObjectNode.Builder builder = Node.objectNodeBuilder()
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
        if (member.hasTrait(SENSITIVE) || model.getShape(member.getTarget()).map(shape -> shape.hasTrait(SENSITIVE)).orElse(false)) {
            builder.withMember("sensitive", true);
        }
        return builder.build();
    }

    private static Optional<ObjectNode> httpBindingNode(MemberShape member) {
        if (member.hasTrait(HTTP_LABEL)) {
            return Optional.of(Node.objectNodeBuilder()
                    .withMember("location", "label")
                    .withMember("name", member.getMemberName())
                    .build());
        }
        String header = stringTrait(member, HTTP_HEADER);
        if (!header.isEmpty()) {
            return Optional.of(Node.objectNodeBuilder()
                    .withMember("location", "header")
                    .withMember("name", header)
                    .build());
        }
        String query = stringTrait(member, HTTP_QUERY);
        if (!query.isEmpty()) {
            return Optional.of(Node.objectNodeBuilder()
                    .withMember("location", "query")
                    .withMember("name", query)
                    .build());
        }
        return Optional.of(Node.objectNodeBuilder()
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
            nodes.add(Node.objectNodeBuilder()
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
        nodes.sort(Comparator.comparing(node -> node.expectObjectNode().expectStringMember("operationId").getValue()));
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
                .withMember("http", Node.objectNodeBuilder()
                        .withMember("method", requiredString(http, "method", operation))
                        .withMember("path", requiredString(http, "uri", operation))
                        .withMember("successStatus", numberMember(http, "code").orElse(200))
                        .build())
                .withMember("input", input.toString())
                .withMember("output", output.toString())
                .withMember("errors", Node.fromStrings(errors.stream().map(ShapeId::toString).collect(Collectors.toList())))
                .withMember("bindings", bindingsNode(model, input))
                .withMember("verself", verselfNode(model, operation, input))
                .withMember("problems", problemsForErrors(model, errors))
                .build();
    }

    private static ObjectNode bindingsNode(Model model, ShapeId inputId) {
        Shape shape = model.getShape(inputId).orElseThrow(() -> new SmithyBuildException("missing input " + inputId));
        StructureShape input = shape.asStructureShape()
                .orElseThrow(() -> new SmithyBuildException(inputId + " is not a structure input"));
        List<Node> labels = new ArrayList<>();
        List<Node> headers = new ArrayList<>();
        List<Node> queries = new ArrayList<>();
        List<String> documentMembers = new ArrayList<>();
        for (MemberShape member : orderedMembers(input)) {
            if (member.hasTrait(HTTP_LABEL)) {
                labels.add(bindingMemberNode(member, member.getMemberName()));
            } else if (!stringTrait(member, HTTP_HEADER).isEmpty()) {
                headers.add(bindingMemberNode(member, stringTrait(member, HTTP_HEADER)));
            } else if (!stringTrait(member, HTTP_QUERY).isEmpty()) {
                queries.add(bindingMemberNode(member, stringTrait(member, HTTP_QUERY)));
            } else {
                documentMembers.add(member.getMemberName());
            }
        }
        documentMembers.sort(String::compareTo);
        return Node.objectNodeBuilder()
                .withMember("labels", Node.fromNodes(labels))
                .withMember("headers", Node.fromNodes(headers))
                .withMember("queries", Node.fromNodes(queries))
                .withMember("documentMembers", Node.fromStrings(documentMembers))
                .build();
    }

    private static ObjectNode bindingMemberNode(MemberShape member, String wireName) {
        return Node.objectNodeBuilder()
                .withMember("member", member.getMemberName())
                .withMember("name", wireName)
                .build();
    }

    private static ObjectNode verselfNode(Model model, OperationShape operation, ShapeId inputId) {
        ObjectNode identity = requiredObjectTrait(operation, IDENTITY);
        ObjectNode authz = requiredObjectTrait(operation, AUTHZ);
        ObjectNode audit = requiredObjectTrait(operation, AUDIT);
        ObjectNode rateLimit = requiredObjectTrait(operation, RATE_LIMIT);
        ObjectNode requestBudget = requiredObjectTrait(operation, REQUEST_BUDGET);
        ObjectNode sdk = requiredObjectTrait(operation, SDK);
        ObjectNode conformance = requiredObjectTrait(operation, CONFORMANCE);

        String permissionShapeId = requiredString(authz, "permission", operation);
        String auditEventShapeId = requiredString(audit, "event", operation);
        String auditResourceShapeId = requiredString(audit, "resource", operation);
        ObjectNode organization = authz.expectObjectMember("organization");
        Optional<ObjectNode> idempotency = idempotencyNode(model, inputId);

        ObjectNode.Builder builder = Node.objectNodeBuilder()
                .withMember("identity", Node.objectNodeBuilder()
                        .withMember("mode", requiredString(identity, "mode", operation))
                        .withMember("audience", requiredString(identity, "audience", operation))
                        .withMember("principals", requiredArray(identity, "principals", operation))
                        .build())
                .withMember("authz", Node.objectNodeBuilder()
                        .withMember("permission", permissionName(model, permissionShapeId, operation))
                        .withMember("permissionShape", permissionShapeId)
                        .withMember("organization", organization)
                        .build())
                .withMember("audit", Node.objectNodeBuilder()
                        .withMember("event", auditEventName(model, auditEventShapeId, operation))
                        .withMember("eventShape", auditEventShapeId)
                        .withMember("resource", lowerSnake(localName(auditResourceShapeId)))
                        .withMember("resourceShape", auditResourceShapeId)
                        .withMember("action", requiredString(audit, "action", operation))
                        .build())
                .withMember("rateLimit", rateLimit)
                .withMember("requestBudget", requestBudget)
                .withMember("sdk", sdk)
                .withMember("conformance", requiredArray(conformance, "cases", operation));
        idempotency.ifPresent(value -> builder.withMember("idempotency", value));
        return builder.build();
    }

    private static Optional<ObjectNode> idempotencyNode(Model model, ShapeId inputId) {
        Shape shape = model.getShape(inputId).orElseThrow(() -> new SmithyBuildException("missing input " + inputId));
        StructureShape input = shape.asStructureShape()
                .orElseThrow(() -> new SmithyBuildException(inputId + " is not a structure input"));
        for (MemberShape member : input.getAllMembers().values()) {
            if (!member.hasTrait(IDEMPOTENCY_TOKEN)) {
                continue;
            }
            String header = stringTrait(member, HTTP_HEADER);
            if (header.isEmpty()) {
                throw new SmithyBuildException(member.getId() + " has @idempotencyToken but no @httpHeader");
            }
            return Optional.of(Node.objectNodeBuilder()
                    .withMember("policy", "idempotency_key_header")
                    .withMember("header", header)
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
        return Node.fromNodes(problems.values().stream()
                .sorted(Comparator.comparing(node -> node.expectObjectNode().expectStringMember("shapeId").getValue()))
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
        Shape shape = model.getShape(errorId).orElseThrow(() -> new SmithyBuildException("missing error " + errorId));
        ObjectNode problem = requiredObjectTrait(shape, PROBLEM);
        return Node.objectNodeBuilder()
                .withMember("shapeId", errorId.toString())
                .withMember("name", errorId.getName())
                .withMember("status", httpErrorStatus(shape))
                .withMember("type", requiredString(problem, "type", shape))
                .withMember("code", requiredString(problem, "code", shape))
                .build();
    }

    private static ObjectNode catalogsNode(Model model, ServiceShape service, List<OperationShape> operations) {
        String serviceName = requiredString(requiredObjectTrait(service, SERVICE_RUNTIME), "serviceName", service);
        List<Node> iam = new ArrayList<>();
        List<Node> audit = new ArrayList<>();
        List<Node> observability = new ArrayList<>();
        for (OperationShape operation : operations) {
            ObjectNode verself = verselfNode(model, operation, operation.getInput().orElseThrow(() -> missing(operation, "input target")));
            ObjectNode authz = verself.expectObjectMember("authz");
            ObjectNode auditNode = verself.expectObjectMember("audit");
            ObjectNode organization = authz.expectObjectMember("organization");
            ObjectNode http = requiredObjectTrait(operation, HTTP);
            String operationId = lowerKebab(operation.getId().getName());
            String orgScope = runtimeOrgScope(requiredString(organization, "source", operation));

            iam.add(Node.objectNodeBuilder()
                    .withMember("operationId", operationId)
                    .withMember("permission", requiredString(authz, "permission", operation))
                    .withMember("resource", requiredString(auditNode, "resource", operation))
                    .withMember("action", requiredString(auditNode, "action", operation))
                    .withMember("orgScope", orgScope)
                    .withMember("memberEligible", memberEligible(requiredString(auditNode, "action", operation)))
                    .build());
            audit.add(Node.objectNodeBuilder()
                    .withMember("operationId", operationId)
                    .withMember("event", requiredString(auditNode, "event", operation))
                    .withMember("resource", requiredString(auditNode, "resource", operation))
                    .withMember("resourceShape", requiredString(auditNode, "resourceShape", operation))
                    .withMember("action", requiredString(auditNode, "action", operation))
                    .withMember("orgScope", orgScope)
                    .build());
            observability.add(Node.objectNodeBuilder()
                    .withMember("operationId", operationId)
                    .withMember("service", serviceName)
                    .withMember("httpMethod", requiredString(http, "method", operation))
                    .withMember("httpRoute", requiredString(http, "uri", operation))
                    .withMember("permission", requiredString(authz, "permission", operation))
                    .withMember("auditEvent", requiredString(auditNode, "event", operation))
                    .withMember("rateLimitBucket", requiredString(verself.expectObjectMember("rateLimit"), "bucket", operation))
                    .withMember("bodyBudgetBytes", numberMember(verself.expectObjectMember("requestBudget"), "maxBytes").orElse(0))
                    .withMember("conformance", verself.expectArrayMember("conformance"))
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
            List<ResourceShape> resources
    ) {
        ObjectNode.Builder builder = Node.objectNodeBuilder()
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

    private static Set<ShapeId> shapeClosure(Model model, List<OperationShape> operations, List<ResourceShape> resources) {
        Set<ShapeId> seen = new LinkedHashSet<>();
        for (OperationShape operation : operations) {
            operation.getInput().ifPresent(id -> visitShape(model, id, seen));
            operation.getOutput().ifPresent(id -> visitShape(model, id, seen));
            for (ShapeId error : operation.getErrorsSet()) {
                visitShape(model, error, seen);
            }
        }
        for (ResourceShape resource : resources) {
            resource.getIdentifiers().values().forEach(id -> visitShape(model, id, seen));
            resource.getProperties().values().forEach(id -> visitShape(model, id, seen));
        }
        return seen;
    }

    private static void visitShape(Model model, ShapeId id, Set<ShapeId> seen) {
        if (id.getNamespace().equals("smithy.api") || !seen.add(id)) {
            return;
        }
        Shape shape = model.getShape(id).orElse(null);
        if (shape == null) {
            return;
        }
        if (shape.isStructureShape()) {
            for (MemberShape member : shape.asStructureShape().get().getAllMembers().values()) {
                visitShape(model, member.getTarget(), seen);
            }
        } else if (shape.isListShape()) {
            visitShape(model, shape.asListShape().get().getMember().getTarget(), seen);
        }
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
        return shape.findTrait(ShapeId.from(HTTP_ERROR))
                .map(Trait::toNode)
                .flatMap(Node::asNumberNode)
                .map(number -> number.getValue().intValue())
                .orElse(500);
    }

    private static String permissionName(Model model, String permissionShapeId, Shape owner) {
        Shape permissionShape = model.getShape(ShapeId.from(permissionShapeId))
                .orElseThrow(() -> new SmithyBuildException(owner.getId() + " references missing permission " + permissionShapeId));
        return requiredString(requiredObjectTrait(permissionShape, PERMISSION), "name", permissionShape);
    }

    private static String auditEventName(Model model, String eventShapeId, Shape owner) {
        Shape eventShape = model.getShape(ShapeId.from(eventShapeId))
                .orElseThrow(() -> new SmithyBuildException(owner.getId() + " references missing audit event " + eventShapeId));
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

    private static ObjectNode requiredObjectTrait(Shape shape, ShapeId id) {
        return objectTrait(shape, id)
                .orElseThrow(() -> new SmithyBuildException(shape.getId() + " is missing " + id));
    }

    private static ObjectNode requiredObjectTrait(Shape shape, String id) {
        return objectTrait(shape, id)
                .orElseThrow(() -> new SmithyBuildException(shape.getId() + " is missing " + id));
    }

    private static Optional<ObjectNode> objectTrait(Shape shape, ShapeId traitId) {
        return objectTrait(shape, traitId.toString());
    }

    private static Optional<ObjectNode> objectTrait(Shape shape, String traitId) {
        return shape.findTrait(ShapeId.from(traitId))
                .map(Trait::toNode)
                .flatMap(Node::asObjectNode);
    }

    private static String stringTrait(Shape shape, String traitId) {
        return shape.findTrait(ShapeId.from(traitId))
                .map(Trait::toNode)
                .flatMap(Node::asStringNode)
                .map(node -> node.getValue())
                .orElse("");
    }

    private static String requiredString(ObjectNode node, String member, Shape owner) {
        return node.getStringMember(member)
                .map(string -> string.getValue())
                .filter(value -> !value.isEmpty())
                .orElseThrow(() -> new SmithyBuildException(owner.getId() + " missing required member `" + member + "`"));
    }

    private static Node requiredArray(ObjectNode node, String member, Shape owner) {
        return node.getArrayMember(member)
                .orElseThrow(() -> new SmithyBuildException(owner.getId() + " missing required array `" + member + "`"));
    }

    private static Optional<Integer> numberMember(ObjectNode node, String member) {
        return node.getNumberMember(member).map(number -> number.getValue().intValue());
    }

    private static Optional<String> stringSetting(ObjectNode settings, String member) {
        return settings.getStringMember(member).map(stringNode -> stringNode.getValue());
    }

    private static <T extends Shape> List<T> sortedShapes(Collection<T> shapes) {
        return shapes.stream()
                .sorted(Comparator.comparing(shape -> shape.getId().toString()))
                .collect(Collectors.toList());
    }

    private static SmithyBuildException missing(Shape shape, String what) {
        return new SmithyBuildException(shape.getId() + " missing " + what);
    }

    private static String localName(String id) {
        int idx = id.lastIndexOf('#');
        return idx >= 0 ? id.substring(idx + 1) : id;
    }

    private static String lowerKebab(String name) {
        return lowerSnake(name).replace('_', '-');
    }

    private static String lowerSnake(String name) {
        String out = WORD_BOUNDARY.matcher(name).replaceAll("$1_$2");
        out = out.replace('-', '_').replace('.', '_');
        return out.toLowerCase();
    }
}
