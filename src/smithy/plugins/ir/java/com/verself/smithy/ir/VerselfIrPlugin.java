package com.verself.smithy.ir;

import java.util.Collection;
import java.util.List;
import java.util.Optional;
import java.util.stream.Collectors;
import software.amazon.smithy.build.PluginContext;
import software.amazon.smithy.build.SmithyBuildException;
import software.amazon.smithy.build.SmithyBuildPlugin;
import software.amazon.smithy.model.Model;
import software.amazon.smithy.model.knowledge.TopDownIndex;
import software.amazon.smithy.model.node.Node;
import software.amazon.smithy.model.node.ObjectNode;
import software.amazon.smithy.model.shapes.ServiceShape;
import software.amazon.smithy.model.shapes.Shape;
import software.amazon.smithy.model.shapes.ShapeId;
import software.amazon.smithy.model.traits.Trait;

/**
 * Emits the Verself contract IR for a projected Smithy model.
 */
public final class VerselfIrPlugin implements SmithyBuildPlugin {
    private static final String IR_VERSION = "verself.contract-ir.v0";
    private static final ShapeId SERVICE_RUNTIME = ShapeId.from("verself.common.v1#serviceRuntime");

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

        ObjectNode ir = Node.objectNodeBuilder()
                .withMember("irVersion", IR_VERSION)
                .withMember("package", packageName)
                .withMember("projection", projection)
                .withMember("visibility", visibility)
                .withMember("service", serviceNode(service))
                .withMember("operations", Node.fromStrings(shapeIds(topDown.getContainedOperations(service))))
                .withMember("resources", Node.fromStrings(shapeIds(topDown.getContainedResources(service))))
                .withMember("problems", Node.arrayNode())
                .withMember("shapes", Node.objectNodeBuilder()
                        .withMember("count", model.getShapeIds().size())
                        .build())
                .withMember("source", Node.objectNode())
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

        List<ServiceShape> services = model.getServiceShapes().stream()
                .sorted((left, right) -> left.getId().compareTo(right.getId()))
                .collect(Collectors.toList());
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

    private static Optional<ObjectNode> objectTrait(Shape shape, ShapeId traitId) {
        return shape.findTrait(traitId)
                .map(Trait::toNode)
                .flatMap(Node::asObjectNode);
    }

    private static Optional<String> stringSetting(ObjectNode settings, String member) {
        return settings.getStringMember(member).map(stringNode -> stringNode.getValue());
    }

    private static List<String> shapeIds(Collection<? extends Shape> shapes) {
        return shapes.stream()
                .map(shape -> shape.getId().toString())
                .sorted()
                .collect(Collectors.toList());
    }
}
