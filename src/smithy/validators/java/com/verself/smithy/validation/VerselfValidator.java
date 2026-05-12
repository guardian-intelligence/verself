package com.verself.smithy.validation;

import java.util.ArrayList;
import java.util.HashMap;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.Set;
import software.amazon.smithy.model.Model;
import software.amazon.smithy.model.knowledge.OperationIndex;
import software.amazon.smithy.model.knowledge.TopDownIndex;
import software.amazon.smithy.model.node.Node;
import software.amazon.smithy.model.node.ObjectNode;
import software.amazon.smithy.model.shapes.MemberShape;
import software.amazon.smithy.model.shapes.OperationShape;
import software.amazon.smithy.model.shapes.ResourceShape;
import software.amazon.smithy.model.shapes.ServiceShape;
import software.amazon.smithy.model.shapes.Shape;
import software.amazon.smithy.model.shapes.ShapeId;
import software.amazon.smithy.model.shapes.StructureShape;
import software.amazon.smithy.model.traits.HttpHeaderTrait;
import software.amazon.smithy.model.traits.IdempotencyTokenTrait;
import software.amazon.smithy.model.traits.IdempotentTrait;
import software.amazon.smithy.model.traits.ReadonlyTrait;
import software.amazon.smithy.model.traits.Trait;
import software.amazon.smithy.model.validation.AbstractValidator;
import software.amazon.smithy.model.validation.ValidationEvent;
import software.amazon.smithy.model.validation.ValidatorService;

/**
 * Repository-specific semantic checks for Verself Smithy contracts.
 */
public final class VerselfValidator extends AbstractValidator {
    private static final ShapeId AUDIT = ShapeId.from("verself.common.v1#audit");
    private static final ShapeId AUTHZ = ShapeId.from("verself.common.v1#authz");
    private static final ShapeId CONFORMANCE = ShapeId.from("verself.common.v1#conformance");
    private static final ShapeId PROTO_FIELD = ShapeId.from("verself.common.v1#protoField");
    private static final ShapeId REQUEST_BUDGET = ShapeId.from("verself.common.v1#requestBudget");
    private static final ShapeId SDK = ShapeId.from("verself.common.v1#sdk");

    public VerselfValidator(ObjectNode configuration) {}

    @Override
    public List<ValidationEvent> validate(Model model) {
        ContractIndex index = ContractIndex.of(model);
        List<ValidationEvent> events = new ArrayList<>();

        for (OperationShape operation : model.getOperationShapes()) {
            validateAuthz(model, index, operation, events);
            validateAudit(index, operation, events);
            validateIdempotency(index, operation, events);
            validateReadBudget(operation, events);
            validateSdk(operation, events);
        }

            for (StructureShape structure : model.getStructureShapes()) {
                validateProtoFields(structure, events);
            }

        return events;
    }

    private void validateAuthz(
            Model model,
            ContractIndex index,
            OperationShape operation,
            List<ValidationEvent> events
    ) {
        Optional<ObjectNode> authz = objectTrait(operation, AUTHZ);
        if (authz.isEmpty()) {
            return;
        }

        Optional<String> permission = stringMember(authz.get(), "permission");
        if (permission.isPresent() && model.getShape(ShapeId.from(permission.get())).isEmpty()) {
            events.add(danger(
                    operation,
                    "The authz permission `" + permission.get() + "` does not resolve to a modeled permission shape.",
                    "Authz",
                    "MissingPermission"));
        }

        Optional<ObjectNode> organization = authz.get().getObjectMember("organization");
        if (organization.isEmpty()) {
            return;
        }

        String source = stringMember(organization.get(), "source").orElse("");
        if (!source.equals("input_member")) {
            return;
        }

        Optional<String> member = stringMember(organization.get(), "member");
        if (member.isEmpty()) {
            events.add(danger(
                    operation,
                    "Authz organization source `input_member` requires an organization.member value.",
                    "Authz",
                    "MissingOrgBindingMember"));
            return;
        }

        if (!index.inputMembers(operation).containsKey(member.get())) {
            events.add(danger(
                    operation,
                    "Authz organization member `" + member.get() + "` is not present in the operation input.",
                    "Authz",
                    "InvalidOrgBinding"));
        }
    }

    private void validateAudit(ContractIndex index, OperationShape operation, List<ValidationEvent> events) {
        Optional<ObjectNode> audit = objectTrait(operation, AUDIT);
        if (audit.isEmpty()) {
            return;
        }

        Optional<String> resource = stringMember(audit.get(), "resource");
        if (resource.isEmpty()) {
            events.add(danger(
                    operation,
                    "Audit metadata must identify the governed resource shape.",
                    "Audit",
                    "MissingResource"));
            return;
        }

        ShapeId resourceId = ShapeId.from(resource.get());
        if (!index.resourcesForOperation(operation).contains(resourceId)) {
            events.add(danger(
                    operation,
                    "Audit resource `" + resourceId + "` is not in the operation's service resource closure.",
                    "Audit",
                    "ResourceOutsideClosure"));
        }
    }

    private void validateIdempotency(ContractIndex index, OperationShape operation, List<ValidationEvent> events) {
        if (!operation.hasTrait(IdempotentTrait.class)) {
            return;
        }

        List<MemberShape> tokens = idempotencyTokenMembers(index, operation);
        if (tokens.size() != 1) {
            events.add(danger(
                    operation,
                    "Idempotent operations must define exactly one @idempotencyToken input member.",
                    "Idempotency",
                    "MissingToken"));
            return;
        }

        MemberShape token = tokens.get(0);
        Optional<HttpHeaderTrait> header = token.getTrait(HttpHeaderTrait.class);
        if (header.isEmpty() || !header.get().getValue().equalsIgnoreCase("Idempotency-Key")) {
            events.add(danger(
                    token,
                    "The idempotency token member must be bound to the Idempotency-Key header.",
                    "Idempotency",
                    "MissingHeader"));
        }

        if (!conformanceCases(operation).contains("idempotency")) {
            events.add(danger(
                    operation,
                    "Idempotent operations must include the idempotency conformance case.",
                    "Idempotency",
                    "MissingConformance"));
        }
    }

    private void validateReadBudget(OperationShape operation, List<ValidationEvent> events) {
        if (!operation.hasTrait(ReadonlyTrait.class)) {
            return;
        }

        Optional<ObjectNode> budget = objectTrait(operation, REQUEST_BUDGET);
        if (budget.isEmpty()) {
            return;
        }

        long maxBytes = budget.get().getNumberMemberOrDefault("maxBytes", -1).longValue();
        if (maxBytes != 0) {
            events.add(danger(
                    operation,
                    "Readonly operations must use @requestBudget(maxBytes: 0).",
                    "Http",
                    "ReadonlyBodyBudget"));
        }
    }

    private void validateSdk(OperationShape operation, List<ValidationEvent> events) {
        Optional<ObjectNode> sdk = objectTrait(operation, SDK);
        if (sdk.isEmpty()) {
            return;
        }

        boolean retryable = sdk.get().getBooleanMemberOrDefault("retryable");
        boolean paginated = sdk.get().getBooleanMemberOrDefault("paginated");

        if (retryable && !operation.hasTrait(ReadonlyTrait.class) && !operation.hasTrait(IdempotentTrait.class)) {
            events.add(danger(
                    operation,
                    "SDK retryable operations must be readonly or explicitly idempotent.",
                    "Sdk",
                    "RetryMismatch"));
        }

        boolean hasPaginatedTrait = operation.hasTrait("paginated");
        if (paginated != hasPaginatedTrait) {
            events.add(danger(
                    operation,
                    "The @sdk paginated value must match the presence of @paginated.",
                    "Sdk",
                    "PaginationMismatch"));
        }

        Set<String> cases = conformanceCases(operation);
        if (retryable && !cases.contains("retry")) {
            events.add(danger(
                    operation,
                    "SDK retryable operations must include the retry conformance case.",
                    "Sdk",
                    "MissingRetryConformance"));
        }
        if (paginated && !cases.contains("pagination")) {
            events.add(danger(
                    operation,
                    "Paginated SDK operations must include the pagination conformance case.",
                    "Sdk",
                    "MissingPaginationConformance"));
        }
    }

    private void validateProtoFields(StructureShape structure, List<ValidationEvent> events) {
        Map<Integer, MemberShape> seen = new HashMap<>();
        for (MemberShape member : structure.getAllMembers().values()) {
            Optional<ObjectNode> protoField = objectTrait(member, PROTO_FIELD);
            if (protoField.isEmpty()) {
                continue;
            }

            int number = protoField.get().getNumberMemberOrDefault("number", -1).intValue();
            MemberShape previous = seen.putIfAbsent(number, member);
            if (previous != null) {
                events.add(danger(
                        member,
                        "Duplicate @protoField number " + number + " also used by member `"
                                + previous.getMemberName() + "`.",
                        "Proto",
                        "DuplicateFieldNumber"));
            }
        }
    }

    private static Optional<ObjectNode> objectTrait(Shape shape, ShapeId traitId) {
        return shape.findTrait(traitId)
                .map(Trait::toNode)
                .map(Node::expectObjectNode);
    }

    private static Optional<String> stringMember(ObjectNode node, String member) {
        return node.getStringMember(member).map(stringNode -> stringNode.getValue());
    }

    private static Set<String> conformanceCases(OperationShape operation) {
        Optional<ObjectNode> conformance = objectTrait(operation, CONFORMANCE);
        if (conformance.isEmpty()) {
            return Set.of();
        }

        Set<String> cases = new HashSet<>();
        conformance.get().getArrayMember("cases").ifPresent(array -> {
            for (Node node : array.getElements()) {
                cases.add(node.expectStringNode().getValue());
            }
        });
        return cases;
    }

    private static List<MemberShape> idempotencyTokenMembers(ContractIndex index, OperationShape operation) {
        List<MemberShape> tokens = new ArrayList<>();
        index.inputMembers(operation).values().forEach(member -> {
            if (member.hasTrait(IdempotencyTokenTrait.class)) {
                tokens.add(member);
            }
        });
        return tokens;
    }

    public static final class Provider extends ValidatorService.Provider {
        public Provider() {
            super(VerselfValidator.class, VerselfValidator::new);
        }
    }

    private static final class ContractIndex {
        private final OperationIndex operationIndex;
        private final Map<ShapeId, Set<ShapeId>> resourcesByOperation;

        private ContractIndex(Model model) {
            operationIndex = OperationIndex.of(model);
            resourcesByOperation = buildResourcesByOperation(model);
        }

        static ContractIndex of(Model model) {
            return new ContractIndex(model);
        }

        Map<String, MemberShape> inputMembers(OperationShape operation) {
            return operationIndex.getInputMembers(operation);
        }

        Set<ShapeId> resourcesForOperation(OperationShape operation) {
            return resourcesByOperation.getOrDefault(operation.getId(), Set.of());
        }

        private static Map<ShapeId, Set<ShapeId>> buildResourcesByOperation(Model model) {
            TopDownIndex topDown = TopDownIndex.of(model);
            Map<ShapeId, Set<ShapeId>> result = new LinkedHashMap<>();

            for (ServiceShape service : model.getServiceShapes()) {
                Set<ShapeId> serviceResources = new HashSet<>();
                for (ResourceShape resource : topDown.getContainedResources(service)) {
                    serviceResources.add(resource.getId());
                }

                for (OperationShape operation : topDown.getContainedOperations(service)) {
                    result.computeIfAbsent(operation.getId(), ignored -> new HashSet<>()).addAll(serviceResources);
                }
            }

            for (ResourceShape resource : model.getResourceShapes()) {
                Set<ShapeId> resourceClosure = new HashSet<>();
                resourceClosure.add(resource.getId());
                for (ResourceShape contained : topDown.getContainedResources(resource)) {
                    resourceClosure.add(contained.getId());
                }

                for (OperationShape operation : topDown.getContainedOperations(resource)) {
                    result.computeIfAbsent(operation.getId(), ignored -> new HashSet<>()).addAll(resourceClosure);
                }
            }

            return result;
        }
    }
}
