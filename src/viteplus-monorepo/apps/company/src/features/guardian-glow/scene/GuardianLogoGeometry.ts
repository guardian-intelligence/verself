import { WINGS_PATH_D } from "@verself/brand";
import { Box3, ExtrudeGeometry, Vector3 } from "three";
import { SVGLoader } from "three/examples/jsm/loaders/SVGLoader.js";

export function createGuardianLogoGeometries(): ExtrudeGeometry[] {
  const loader = new SVGLoader();
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="105 106 140 140"><path d="${WINGS_PATH_D}" /></svg>`;
  const data = loader.parse(svg);
  const geometries = data.paths.flatMap((path) =>
    SVGLoader.createShapes(path).map(
      (shape) =>
        new ExtrudeGeometry(shape, {
          bevelEnabled: true,
          bevelSegments: 22,
          bevelSize: 1.45,
          bevelThickness: 3.2,
          curveSegments: 128,
          depth: 7.4,
        }),
    ),
  );

  const bounds = new Box3();
  for (const geometry of geometries) {
    geometry.computeBoundingBox();
    const box = geometry.boundingBox;
    if (box) {
      bounds.expandByPoint(box.min);
      bounds.expandByPoint(box.max);
    }
  }

  const center = bounds.getCenter(new Vector3());
  const sourceHeight = Math.max(1, bounds.max.y - bounds.min.y);
  const scale = 4.25 / sourceHeight;

  for (const geometry of geometries) {
    geometry.translate(-center.x, -center.y, -center.z);
    geometry.scale(scale, -scale, scale);
    curveGuardianSurface(geometry);
    geometry.computeVertexNormals();
    geometry.computeBoundingBox();
    geometry.computeBoundingSphere();
  }

  return geometries;
}

function curveGuardianSurface(geometry: ExtrudeGeometry) {
  const position = geometry.attributes.position;
  if (!position) return;

  for (let i = 0; i < position.count; i += 1) {
    const x = position.getX(i);
    const y = position.getY(i);
    const z = position.getZ(i);
    const dome = 0.22 * Math.exp(-0.22 * (x * x + y * y * 0.58));
    const diagonalCrown = 0.1 * Math.sin((x * 0.82 + y * 0.56) * 1.45);
    const lowerLift = 0.1 * Math.exp(-1.6 * ((x + 1.1) ** 2 + (y + 1.35) ** 2));

    position.setZ(i, z + dome + diagonalCrown + lowerLift);
  }

  position.needsUpdate = true;
}
