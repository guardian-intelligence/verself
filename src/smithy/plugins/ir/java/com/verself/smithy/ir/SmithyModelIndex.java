package com.verself.smithy.ir;

import java.util.LinkedHashSet;
import java.util.List;
import java.util.Set;
import software.amazon.smithy.build.SmithyBuildException;
import software.amazon.smithy.model.Model;
import software.amazon.smithy.model.knowledge.TopDownIndex;
import software.amazon.smithy.model.node.ObjectNode;
import software.amazon.smithy.model.shapes.MemberShape;
import software.amazon.smithy.model.shapes.OperationShape;
import software.amazon.smithy.model.shapes.ResourceShape;
import software.amazon.smithy.model.shapes.ServiceShape;
import software.amazon.smithy.model.shapes.Shape;
import software.amazon.smithy.model.shapes.ShapeId;

final class SmithyModelIndex {
  private final Model model;
  private final ServiceShape service;
  private final List<OperationShape> operations;
  private final List<ResourceShape> resources;
  private final List<Shape> shapes;

  private SmithyModelIndex(
      Model model,
      ServiceShape service,
      List<OperationShape> operations,
      List<ResourceShape> resources,
      List<Shape> shapes) {
    this.model = model;
    this.service = service;
    this.operations = List.copyOf(operations);
    this.resources = List.copyOf(resources);
    this.shapes = List.copyOf(shapes);
  }

  static SmithyModelIndex create(Model model, ObjectNode settings) {
    ServiceShape service = resolveService(model, settings);
    TopDownIndex topDown = TopDownIndex.of(model);
    List<OperationShape> operations =
        SmithyNodes.sortedShapes(topDown.getContainedOperations(service));
    List<ResourceShape> resources =
        SmithyNodes.sortedShapes(topDown.getContainedResources(service));
    Set<ShapeId> shapeClosure = shapeClosure(model, operations, resources);
    List<Shape> shapes =
        shapeClosure.stream()
            .map(
                id ->
                    model
                        .getShape(id)
                        .orElseThrow(() -> new SmithyBuildException("missing shape " + id)))
            .sorted((left, right) -> left.getId().toString().compareTo(right.getId().toString()))
            .toList();
    return new SmithyModelIndex(model, service, operations, resources, shapes);
  }

  Model model() {
    return model;
  }

  ServiceShape service() {
    return service;
  }

  List<OperationShape> operations() {
    return operations;
  }

  List<ResourceShape> resources() {
    return resources;
  }

  List<Shape> shapes() {
    return shapes;
  }

  private static ServiceShape resolveService(Model model, ObjectNode settings) {
    return SmithyNodes.stringSetting(settings, "service")
        .map(ShapeId::from)
        .map(
            id ->
                model
                    .getShape(id)
                    .flatMap(Shape::asServiceShape)
                    .orElseThrow(
                        () ->
                            new SmithyBuildException(
                                "verself-ir service `" + id + "` was not found")))
        .orElseGet(
            () -> {
              List<ServiceShape> services = SmithyNodes.sortedShapes(model.getServiceShapes());
              if (services.size() != 1) {
                throw new SmithyBuildException(
                    "verself-ir requires a `service` setting when the projection contains "
                        + services.size()
                        + " services");
              }
              return services.get(0);
            });
  }

  private static Set<ShapeId> shapeClosure(
      Model model, List<OperationShape> operations, List<ResourceShape> resources) {
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
}
