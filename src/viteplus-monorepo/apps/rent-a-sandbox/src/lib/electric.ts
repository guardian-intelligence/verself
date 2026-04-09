import { createCollection } from "@tanstack/db";
import {
  electricCollectionOptions,
  type ElectricCollectionConfig,
} from "@tanstack/electric-db-collection";
import type { Row } from "@electric-sql/client";

export function createElectricCollection<T extends Row<unknown>>(config: ElectricCollectionConfig<T>) {
  return createCollection(electricCollectionOptions(config));
}
