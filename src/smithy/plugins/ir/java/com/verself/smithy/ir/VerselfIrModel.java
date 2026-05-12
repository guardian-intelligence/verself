package com.verself.smithy.ir;

import software.amazon.smithy.build.PluginContext;
import software.amazon.smithy.model.node.ObjectNode;
import software.amazon.smithy.model.shapes.ServiceShape;

record VerselfIrModel(
    String packageName,
    String projection,
    String visibility,
    String filename,
    SmithyModelIndex index) {
  static VerselfIrModel create(PluginContext context, SmithyModelIndex index) {
    ObjectNode settings = context.getSettings();
    ServiceShape service = index.service();
    String packageName =
        SmithyNodes.stringSetting(settings, "package").orElse(service.getId().getNamespace());
    String projection = context.getProjectionName();
    String visibility = SmithyNodes.stringSetting(settings, "visibility").orElse(projection);
    String filename =
        SmithyNodes.stringSetting(settings, "filename")
            .orElse("ir/" + packageName + "/" + projection + ".json");
    return new VerselfIrModel(packageName, projection, visibility, filename, index);
  }
}
