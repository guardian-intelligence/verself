package com.verself.smithy.ir;

import java.util.regex.Pattern;

final class SmithyNames {
  private static final Pattern WORD_BOUNDARY = Pattern.compile("([a-z0-9])([A-Z])");

  private SmithyNames() {}

  static String localName(String id) {
    int idx = id.lastIndexOf('#');
    return idx >= 0 ? id.substring(idx + 1) : id;
  }

  static String lowerKebab(String name) {
    return lowerSnake(name).replace('_', '-');
  }

  static String lowerSnake(String name) {
    String out = WORD_BOUNDARY.matcher(name).replaceAll("$1_$2");
    out = out.replace('-', '_').replace('.', '_');
    return out.toLowerCase();
  }
}
