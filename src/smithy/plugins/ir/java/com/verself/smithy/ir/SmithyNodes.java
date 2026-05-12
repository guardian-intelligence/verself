package com.verself.smithy.ir;

import java.util.Collection;
import java.util.Comparator;
import java.util.List;
import java.util.Optional;
import java.util.stream.Collectors;
import software.amazon.smithy.build.SmithyBuildException;
import software.amazon.smithy.model.node.Node;
import software.amazon.smithy.model.node.ObjectNode;
import software.amazon.smithy.model.shapes.Shape;
import software.amazon.smithy.model.shapes.ShapeId;
import software.amazon.smithy.model.traits.Trait;

final class SmithyNodes {
  private SmithyNodes() {}

  static ObjectNode requiredObjectTrait(Shape shape, ShapeId id) {
    return objectTrait(shape, id)
        .orElseThrow(() -> new SmithyBuildException(shape.getId() + " is missing " + id));
  }

  static ObjectNode requiredObjectTrait(Shape shape, String id) {
    return objectTrait(shape, id)
        .orElseThrow(() -> new SmithyBuildException(shape.getId() + " is missing " + id));
  }

  static Optional<ObjectNode> objectTrait(Shape shape, ShapeId traitId) {
    return objectTrait(shape, traitId.toString());
  }

  static Optional<ObjectNode> objectTrait(Shape shape, String traitId) {
    return shape.findTrait(ShapeId.from(traitId)).map(Trait::toNode).flatMap(Node::asObjectNode);
  }

  static String stringTrait(Shape shape, String traitId) {
    return shape
        .findTrait(ShapeId.from(traitId))
        .map(Trait::toNode)
        .flatMap(Node::asStringNode)
        .map(node -> node.getValue())
        .orElse("");
  }

  static String requiredString(ObjectNode node, String member, Shape owner) {
    return node.getStringMember(member)
        .map(string -> string.getValue())
        .filter(value -> !value.isEmpty())
        .orElseThrow(
            () ->
                new SmithyBuildException(
                    owner.getId() + " missing required member `" + member + "`"));
  }

  static Node requiredArray(ObjectNode node, String member, Shape owner) {
    return node.getArrayMember(member)
        .orElseThrow(
            () ->
                new SmithyBuildException(
                    owner.getId() + " missing required array `" + member + "`"));
  }

  static Optional<Integer> numberMember(ObjectNode node, String member) {
    return node.getNumberMember(member).map(number -> number.getValue().intValue());
  }

  static Optional<String> stringSetting(ObjectNode settings, String member) {
    return settings.getStringMember(member).map(stringNode -> stringNode.getValue());
  }

  static <T extends Shape> List<T> sortedShapes(Collection<T> shapes) {
    return shapes.stream()
        .sorted(Comparator.comparing(shape -> shape.getId().toString()))
        .collect(Collectors.toList());
  }
}
