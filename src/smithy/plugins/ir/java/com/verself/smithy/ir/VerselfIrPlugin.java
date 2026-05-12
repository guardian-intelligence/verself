package com.verself.smithy.ir;

import software.amazon.smithy.build.PluginContext;
import software.amazon.smithy.build.SmithyBuildPlugin;

/** Emits the Verself contract IR for a projected Smithy model. */
public final class VerselfIrPlugin implements SmithyBuildPlugin {
  @Override
  public String getName() {
    return "verself-ir";
  }

  @Override
  public void execute(PluginContext context) {
    SmithyModelIndex index = SmithyModelIndex.create(context.getModel(), context.getSettings());
    VerselfIrModel contract = VerselfIrModel.create(context, index);

    context.getFileManifest().writeJson(contract.filename(), VerselfIrJsonEmitter.toNode(contract));
  }
}
