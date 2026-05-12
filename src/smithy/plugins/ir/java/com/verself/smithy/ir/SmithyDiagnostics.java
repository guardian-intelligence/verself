package com.verself.smithy.ir;

import software.amazon.smithy.build.SmithyBuildException;
import software.amazon.smithy.model.shapes.Shape;

final class SmithyDiagnostics {
  private SmithyDiagnostics() {}

  static SmithyBuildException missing(Shape shape, String what) {
    return new SmithyBuildException(shape.getId() + " missing " + what);
  }
}
