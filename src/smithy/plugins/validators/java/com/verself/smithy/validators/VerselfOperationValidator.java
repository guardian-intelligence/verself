package com.verself.smithy.validators;

import java.util.ArrayList;
import java.util.Comparator;
import java.util.List;
import java.util.Optional;
import software.amazon.smithy.model.Model;
import software.amazon.smithy.model.node.Node;
import software.amazon.smithy.model.node.ObjectNode;
import software.amazon.smithy.model.shapes.MemberShape;
import software.amazon.smithy.model.shapes.OperationShape;
import software.amazon.smithy.model.shapes.ServiceShape;
import software.amazon.smithy.model.shapes.Shape;
import software.amazon.smithy.model.shapes.ShapeId;
import software.amazon.smithy.model.shapes.StructureShape;
import software.amazon.smithy.model.traits.Trait;
import software.amazon.smithy.model.validation.AbstractValidator;
import software.amazon.smithy.model.validation.ValidationEvent;

/**
 * Keeps Verself operation contracts complete enough for runtime policy and OpenAPI projection.
 *
 * <p>This validator intentionally does not generate any artifact. Smithy remains the contract
 * source of truth; these checks only preserve the metadata that services and SDK tooling rely on.
 */
public final class VerselfOperationValidator extends AbstractValidator {
  private static final String VERSELF_NAMESPACE_PREFIX = "verself.";

  private static final ShapeId AUDIT = ShapeId.from("verself.common.v1#audit");
  private static final ShapeId AUTHZ = ShapeId.from("verself.common.v1#authz");
  private static final ShapeId IDENTITY = ShapeId.from("verself.common.v1#identity");
  private static final ShapeId PROBLEM = ShapeId.from("verself.common.v1#problem");
  private static final ShapeId MULTI_PROBLEM_DETAILS =
      ShapeId.from("verself.common.v1#MultiProblemDetails");
  private static final ShapeId PROVIDER_WEBHOOK_INVALID_REQUEST =
      ShapeId.from("verself.common.v1#ProviderWebhookInvalidRequestError");
  private static final ShapeId PROVIDER_WEBHOOK_SIGNATURE_INVALID =
      ShapeId.from("verself.common.v1#ProviderWebhookSignatureInvalidError");
  private static final ShapeId PROVIDER_WEBHOOK_DELIVERY_REPLAY_CONFLICT =
      ShapeId.from("verself.common.v1#ProviderWebhookDeliveryReplayConflictError");
  private static final ShapeId PROVIDER_WEBHOOK_INBOX_UNAVAILABLE =
      ShapeId.from("verself.common.v1#ProviderWebhookInboxUnavailableError");
  private static final ShapeId RATE_LIMIT = ShapeId.from("verself.common.v1#rateLimit");
  private static final ShapeId REQUEST_BUDGET = ShapeId.from("verself.common.v1#requestBudget");
  private static final ShapeId SDK = ShapeId.from("verself.common.v1#sdk");
  private static final ShapeId SERVICE_RUNTIME = ShapeId.from("verself.common.v1#serviceRuntime");

  private static final ShapeId ERROR = ShapeId.from("smithy.api#error");
  private static final ShapeId HTTP = ShapeId.from("smithy.api#http");
  private static final ShapeId HTTP_ERROR = ShapeId.from("smithy.api#httpError");
  private static final ShapeId HTTP_HEADER = ShapeId.from("smithy.api#httpHeader");
  private static final ShapeId HTTP_LABEL = ShapeId.from("smithy.api#httpLabel");
  private static final ShapeId HTTP_PAYLOAD = ShapeId.from("smithy.api#httpPayload");
  private static final ShapeId HTTP_QUERY = ShapeId.from("smithy.api#httpQuery");
  private static final ShapeId IDEMPOTENCY_TOKEN = ShapeId.from("smithy.api#idempotencyToken");
  private static final ShapeId IDEMPOTENT = ShapeId.from("smithy.api#idempotent");
  private static final ShapeId PAGINATED = ShapeId.from("smithy.api#paginated");

  @Override
  public List<ValidationEvent> validate(Model model) {
    List<ValidationEvent> events = new ArrayList<>();

    for (ServiceShape service : sorted(model.getServiceShapes())) {
      if (isVerselfShape(service)) {
        validateService(service, events);
      }
    }
    for (OperationShape operation : sorted(model.getOperationShapes())) {
      if (isVerselfShape(operation)) {
        validateOperation(model, operation, events);
      }
    }
    return events;
  }

  private void validateService(ServiceShape service, List<ValidationEvent> events) {
    requireTrait(service, SERVICE_RUNTIME, "@serviceRuntime", events);
  }

  private void validateOperation(
      Model model, OperationShape operation, List<ValidationEvent> events) {
    requireTrait(operation, HTTP, "@http", events);
    ObjectNode identity = requireObjectTrait(operation, IDENTITY, "@identity", events).orElse(null);
    ObjectNode authz = requireObjectTrait(operation, AUTHZ, "@authz", events).orElse(null);
    requireTrait(operation, AUDIT, "@audit", events);
    requireTrait(operation, RATE_LIMIT, "@rateLimit", events);
    ObjectNode requestBudget =
        requireObjectTrait(operation, REQUEST_BUDGET, "@requestBudget", events).orElse(null);
    ObjectNode sdk = requireObjectTrait(operation, SDK, "@sdk", events).orElse(null);

    Optional<StructureShape> input =
        resolveStructure(model, operation, operation.getInput(), "input", events);
    Optional<StructureShape> output =
        resolveStructure(model, operation, operation.getOutput(), "output", events);

    if (operation.getErrorsSet().isEmpty()) {
      events.add(error(operation, "Verself operations must declare a stable error set."));
    }
    validateErrors(model, operation, events);
    input.ifPresent(shape -> validateRequestBindings(shape, events));
    output.ifPresent(shape -> validateResponseBindings(shape, events));
    input.ifPresent(shape -> validateIdempotency(operation, shape, events));
    input.ifPresent(shape -> validateOrganizationBinding(model, operation, authz, shape, events));
    input.ifPresent(shape -> validateRequestBudget(operation, requestBudget, shape, events));
    input.ifPresent(
        shape -> validateProviderWebhookOperation(model, operation, identity, shape, events));
    validateSdk(operation, sdk, events);
  }

  private Optional<ObjectNode> requireObjectTrait(
      Shape shape, ShapeId trait, String label, List<ValidationEvent> events) {
    Optional<ObjectNode> node = objectTrait(shape, trait);
    if (node.isEmpty()) {
      events.add(error(shape, "Verself operations must declare " + label + "."));
    }
    return node;
  }

  private void requireTrait(
      Shape shape, ShapeId trait, String label, List<ValidationEvent> events) {
    if (!shape.hasTrait(trait)) {
      events.add(error(shape, "Verself contracts must declare " + label + "."));
    }
  }

  private Optional<StructureShape> resolveStructure(
      Model model,
      OperationShape operation,
      Optional<ShapeId> shapeId,
      String role,
      List<ValidationEvent> events) {
    if (shapeId.isEmpty()) {
      events.add(error(operation, "Verself operations must declare an explicit " + role + "."));
      return Optional.empty();
    }
    Optional<Shape> target = model.getShape(shapeId.get());
    if (target.isEmpty()) {
      events.add(
          error(
              operation,
              "Verself operation references missing " + role + " " + shapeId.get() + "."));
      return Optional.empty();
    }
    Optional<StructureShape> structure = target.get().asStructureShape();
    if (structure.isEmpty()) {
      events.add(error(operation, "Verself operation " + role + " must target a structure."));
    }
    return structure;
  }

  private void validateErrors(Model model, OperationShape operation, List<ValidationEvent> events) {
    for (ShapeId errorId : operation.getErrorsSet().stream().sorted().toList()) {
      Optional<Shape> error = model.getShape(errorId);
      if (error.isEmpty()) {
        events.add(error(operation, "Verself operation references missing error " + errorId + "."));
        continue;
      }
      Shape shape = error.get();
      if (!shape.hasTrait(ERROR)) {
        events.add(error(shape, "Verself operation errors must be marked with @error."));
      }
      if (!shape.hasTrait(HTTP_ERROR)) {
        events.add(error(shape, "Verself operation errors must declare @httpError."));
      }
      if (!shape.hasTrait(PROBLEM)) {
        events.add(error(shape, "Verself operation errors must declare @problem."));
      }
    }
  }

  private void validateRequestBindings(StructureShape input, List<ValidationEvent> events) {
    int payloads = 0;
    boolean hasDocumentMembers = false;
    for (MemberShape member : input.getAllMembers().values()) {
      if (member.hasTrait(HTTP_PAYLOAD)) {
        payloads++;
      } else if (!isNonDocumentRequestBinding(member)) {
        hasDocumentMembers = true;
      }
    }

    if (payloads > 1) {
      events.add(
          error(input, "Verself operation inputs must not contain multiple @httpPayload members."));
    }
    if (payloads > 0 && hasDocumentMembers) {
      events.add(
          error(
              input,
              "Verself operation inputs must not mix @httpPayload with request document members."));
    }
  }

  private void validateResponseBindings(StructureShape output, List<ValidationEvent> events) {
    int payloads = 0;
    boolean hasDocumentMembers = false;
    for (MemberShape member : output.getAllMembers().values()) {
      if (member.hasTrait(HTTP_PAYLOAD)) {
        payloads++;
      } else if (!member.hasTrait(HTTP_HEADER)) {
        hasDocumentMembers = true;
      }
    }

    if (payloads > 1) {
      events.add(
          error(
              output, "Verself operation outputs must not contain multiple @httpPayload members."));
    }
    if (payloads > 0 && hasDocumentMembers) {
      events.add(
          error(
              output,
              "Verself operation outputs must not mix @httpPayload with response document"
                  + " members."));
    }
  }

  private void validateIdempotency(
      OperationShape operation, StructureShape input, List<ValidationEvent> events) {
    List<MemberShape> tokens =
        input.getAllMembers().values().stream()
            .filter(member -> member.hasTrait(IDEMPOTENCY_TOKEN))
            .sorted(Comparator.comparing(MemberShape::getMemberName))
            .toList();

    if (tokens.size() > 1) {
      events.add(
          error(
              input,
              "Verself operation inputs must not contain multiple @idempotencyToken members."));
    }
    if (!tokens.isEmpty() && !operation.hasTrait(IDEMPOTENT)) {
      events.add(
          error(
              operation,
              "Operations with @idempotencyToken input members must declare @idempotent."));
    }
    for (MemberShape token : tokens) {
      if (token.hasTrait(HTTP_LABEL) || token.hasTrait(HTTP_QUERY)) {
        events.add(
            error(
                token,
                "Verself @idempotencyToken members must be request document members or HTTP"
                    + " headers."));
      }
    }
  }

  private void validateOrganizationBinding(
      Model model,
      OperationShape operation,
      ObjectNode authz,
      StructureShape input,
      List<ValidationEvent> events) {
    if (authz == null) {
      return;
    }
    Optional<ObjectNode> organization = authz.getObjectMember("organization");
    if (organization.isEmpty()) {
      return;
    }
    String source = stringMember(organization.get(), "source").orElse("");
    boolean memberBacked = source.equals("input_member") || source.equals("body_org_id");
    Optional<String> memberName =
        stringMember(organization.get(), "member").filter(value -> !value.isEmpty());
    if (memberBacked && memberName.isEmpty()) {
      events.add(
          error(
              operation, "Organization bindings using " + source + " must name an input member."));
      return;
    }
    if (memberName.isPresent() && !inputOrPayloadContainsMember(model, input, memberName.get())) {
      events.add(
          error(
              operation,
              "Organization binding references missing input or payload member `"
                  + memberName.get()
                  + "`."));
    }
  }

  private void validateRequestBudget(
      OperationShape operation,
      ObjectNode requestBudget,
      StructureShape input,
      List<ValidationEvent> events) {
    if (requestBudget == null) {
      return;
    }
    Optional<Long> maxBytes = longMember(requestBudget, "maxBytes");
    if (maxBytes.isEmpty()) {
      return;
    }
    if (maxBytes.get() < 0) {
      events.add(error(operation, "Verself request budgets must not be negative."));
    }
    if (maxBytes.get() == 0 && hasRequestBody(input)) {
      events.add(error(operation, "A zero @requestBudget is only valid for bodyless requests."));
    }
  }

  private void validateSdk(OperationShape operation, ObjectNode sdk, List<ValidationEvent> events) {
    if (sdk == null) {
      return;
    }
    sdk.getBooleanMember("paginated")
        .ifPresent(
            paginated -> {
              boolean smithyPaginated = operation.hasTrait(PAGINATED);
              if (paginated.getValue() != smithyPaginated) {
                events.add(
                    error(
                        operation,
                        "@sdk.paginated must match the Smithy @paginated trait on the operation."));
              }
            });
  }

  private void validateProviderWebhookOperation(
      Model model,
      OperationShape operation,
      ObjectNode identity,
      StructureShape input,
      List<ValidationEvent> events) {
    if (identity == null || !stringMember(identity, "mode").orElse("").equals("provider_webhook")) {
      return;
    }

    long payloads =
        input.getAllMembers().values().stream()
            .filter(member -> member.hasTrait(HTTP_PAYLOAD))
            .count();
    if (payloads != 1) {
      events.add(error(operation, "Provider webhook operations must bind one raw @httpPayload."));
    }
    requireSignatureHeader(input, operation, events);

    for (ShapeId errorId :
        List.of(
            PROVIDER_WEBHOOK_INVALID_REQUEST,
            PROVIDER_WEBHOOK_SIGNATURE_INVALID,
            PROVIDER_WEBHOOK_DELIVERY_REPLAY_CONFLICT,
            PROVIDER_WEBHOOK_INBOX_UNAVAILABLE)) {
      if (!operation.getErrorsSet().contains(errorId)) {
        events.add(
            error(
                operation, "Provider webhook operations must declare " + errorId.getName() + "."));
      }
      boolean hasMultiProblemDetails =
          model
              .getShape(errorId)
              .flatMap(Shape::asStructureShape)
              .filter(shape -> hasMixin(shape, MULTI_PROBLEM_DETAILS))
              .isPresent();
      if (!hasMultiProblemDetails) {
        events.add(error(operation, errorId.getName() + " must include MultiProblemDetails."));
      }
    }
  }

  private void requireSignatureHeader(
      StructureShape input, OperationShape operation, List<ValidationEvent> events) {
    boolean found =
        input.getAllMembers().values().stream()
            .map(member -> stringTrait(member, HTTP_HEADER).orElse(""))
            .anyMatch(header -> header.toLowerCase().contains("signature"));
    if (!found) {
      events.add(
          error(operation, "Provider webhook operations must bind a provider signature header."));
    }
  }

  private static boolean hasRequestBody(StructureShape input) {
    for (MemberShape member : input.getAllMembers().values()) {
      if (member.hasTrait(HTTP_PAYLOAD) || !isNonDocumentRequestBinding(member)) {
        return true;
      }
    }
    return false;
  }

  private static boolean isNonDocumentRequestBinding(MemberShape member) {
    return member.hasTrait(HTTP_LABEL)
        || member.hasTrait(HTTP_HEADER)
        || member.hasTrait(HTTP_QUERY);
  }

  private static boolean inputOrPayloadContainsMember(
      Model model, StructureShape input, String memberName) {
    if (input.getAllMembers().containsKey(memberName)) {
      return true;
    }
    for (MemberShape member : input.getAllMembers().values()) {
      if (!member.hasTrait(HTTP_PAYLOAD)) {
        continue;
      }
      Optional<Shape> payload = model.getShape(member.getTarget());
      if (payload
          .flatMap(Shape::asStructureShape)
          .map(shape -> shape.getAllMembers().containsKey(memberName))
          .orElse(false)) {
        return true;
      }
    }
    return false;
  }

  private static boolean hasMixin(StructureShape shape, ShapeId mixinId) {
    return shape.getMixins().contains(mixinId);
  }

  private static boolean isVerselfShape(Shape shape) {
    String namespace = shape.getId().getNamespace();
    return namespace.startsWith(VERSELF_NAMESPACE_PREFIX) && !namespace.equals("verself.common.v1");
  }

  private static Optional<ObjectNode> objectTrait(Shape shape, ShapeId traitId) {
    return shape.findTrait(traitId).map(Trait::toNode).flatMap(Node::asObjectNode);
  }

  private static Optional<String> stringMember(ObjectNode node, String member) {
    return node.getStringMember(member).map(string -> string.getValue());
  }

  private static Optional<String> stringTrait(Shape shape, ShapeId traitId) {
    return shape
        .findTrait(traitId)
        .map(Trait::toNode)
        .flatMap(Node::asStringNode)
        .map(string -> string.getValue());
  }

  private static Optional<Long> longMember(ObjectNode node, String member) {
    return node.getNumberMember(member).map(number -> number.getValue().longValue());
  }

  private static <T extends Shape> List<T> sorted(Iterable<T> shapes) {
    List<T> result = new ArrayList<>();
    shapes.forEach(result::add);
    result.sort(Comparator.comparing(shape -> shape.getId().toString()));
    return result;
  }
}
