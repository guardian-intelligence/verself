import { createCollection } from "@tanstack/db";
import { electricCollectionOptions } from "@tanstack/electric-db-collection";
import { electricShapeURL } from "@forge-metal/web-env";

export function createElectricShapeCollection<T extends object>({
  id,
  table,
  where,
  getKey,
}: {
  id: string;
  table: string;
  where?: string;
  getKey: (item: T) => string;
}) {
  return createCollection<T>(
    electricCollectionOptions({
      id,
      shapeOptions: {
        url: electricShapeURL(),
        params: where ? { table, where } : { table },
      },
      getKey: (item: Record<string, unknown>) => getKey(item as T),
    }) as any,
  );
}
