#!/usr/bin/env node
// Generates a typed TypeScript transport module from GLSL source files.

import process from "node:process";
import { generateGlslModule, glslModuleConfigFromArgs } from "./glsl-module.ts";

await generateGlslModule(glslModuleConfigFromArgs(process.argv));
