import { MathUtils, Object3D, Quaternion, Vector3 } from "three";
import type { GuardianGlowSettings } from "../guardian-glow-config";
import { cloneLogoAnchor, logoAnchorIds, type LogoAnchorId, type LogoFrame } from "./logo-frame";

export type ReflectionCardTexture = "bar" | "edge" | "field" | "soft";

export type ResolvedReflectionCard = {
  readonly color: readonly [number, number, number];
  readonly id: string;
  readonly opacity: number;
  readonly position: readonly [number, number, number];
  readonly rotation: readonly [number, number, number];
  readonly scale: readonly [number, number, number];
  readonly texture: ReflectionCardTexture;
};

export type ResolvedRectLight = {
  readonly color: string;
  readonly height: number;
  readonly id: string;
  readonly intensity: number;
  readonly position: readonly [number, number, number];
  readonly rotation: readonly [number, number, number];
  readonly width: number;
};

export type ResolvedPointLight = {
  readonly color: string;
  readonly distance: number;
  readonly id: string;
  readonly intensity: number;
  readonly position: readonly [number, number, number];
};

export type ResolvedDebugAnchor = {
  readonly color: string;
  readonly id: LogoAnchorId;
  readonly position: readonly [number, number, number];
};

export type ResolvedEclipseSource = {
  readonly color: readonly [number, number, number];
  readonly id: string;
  readonly intensity: number;
  readonly opacity: number;
  readonly position: readonly [number, number, number];
  readonly radius: number;
  readonly rectHeight: number;
  readonly rectIntensity: number;
  readonly rectWidth: number;
  readonly rotation: readonly [number, number, number];
};

export type GuardianLightingRig = {
  readonly anchorDebug: readonly ResolvedDebugAnchor[];
  readonly cards: readonly ResolvedReflectionCard[];
  readonly eclipseSource: ResolvedEclipseSource;
  readonly motionAmplitude: number;
  readonly motionAxis: Vector3;
  readonly origin: Vector3;
  readonly pointLights: readonly ResolvedPointLight[];
  readonly probePosition: readonly [number, number, number];
  readonly rectLights: readonly ResolvedRectLight[];
};

type OrbitSpec = {
  readonly azimuthDeg: number;
  readonly distanceScale: number;
  readonly elevationDeg: number;
  readonly target: LogoAnchorId;
};

type CardSpec = {
  readonly azimuthOffsetDeg: number;
  readonly color: readonly [number, number, number];
  readonly distanceScale: number;
  readonly elevationOffsetDeg: number;
  readonly heightScale: number;
  readonly id: string;
  readonly opacity: number;
  readonly rollDeg: number;
  readonly target: LogoAnchorId;
  readonly texture: ReflectionCardTexture;
  readonly widthScale: number;
};

type RectLightSpec = {
  readonly azimuthOffsetDeg: number;
  readonly color: string;
  readonly distanceScale: number;
  readonly elevationOffsetDeg: number;
  readonly heightScale: number;
  readonly id: string;
  readonly intensity: number;
  readonly rollDeg: number;
  readonly target: LogoAnchorId;
  readonly widthScale: number;
};

type PointLightSpec = {
  readonly azimuthOffsetDeg: number;
  readonly color: string;
  readonly distance: number;
  readonly distanceScale: number;
  readonly elevationOffsetDeg: number;
  readonly id: string;
  readonly intensity: number;
  readonly target: LogoAnchorId;
};

export function resolveGuardianLightingRig(
  settings: GuardianGlowSettings,
  frame: LogoFrame,
): GuardianLightingRig {
  const direct = settings.lightIntensity;
  const cardGain = settings.cardIntensity;
  const warmGain = 0.65 + settings.warm * 0.42;
  const coolGain = 1.05 + settings.aberration * 0.015;
  const cardSpecs: readonly CardSpec[] = [
    {
      azimuthOffsetDeg: 0,
      color: [cardGain * 2.85, cardGain * 2.82, cardGain * 2.74],
      distanceScale: 1,
      elevationOffsetDeg: 0,
      heightScale: 1.12,
      id: "key-reflection-card",
      opacity: 0.92,
      rollDeg: -2,
      target: "frontCenter",
      texture: "bar",
      widthScale: 0.18,
    },
    {
      azimuthOffsetDeg: 46,
      color: [cardGain * 0.34, cardGain * 0.78, cardGain * 3.25 * coolGain],
      distanceScale: 0.92,
      elevationOffsetDeg: 8,
      heightScale: 0.86,
      id: "right-blue-rim-card",
      opacity: 0.82,
      rollDeg: 6,
      target: "rightEdge",
      texture: "edge",
      widthScale: 0.08,
    },
    {
      azimuthOffsetDeg: -42,
      color: [cardGain * 2.45 * warmGain, cardGain * 1.24 * warmGain, cardGain * 0.34],
      distanceScale: 0.86,
      elevationOffsetDeg: -28,
      heightScale: 0.62,
      id: "lower-warm-kick-card",
      opacity: 0.66,
      rollDeg: -12,
      target: "lowerLeft",
      texture: "soft",
      widthScale: 0.24,
    },
    {
      azimuthOffsetDeg: -18,
      color: [cardGain * 0.82, cardGain * 0.94, cardGain * 1.15],
      distanceScale: 1.18,
      elevationOffsetDeg: 34,
      heightScale: 0.26,
      id: "top-softbox-card",
      opacity: 0.46,
      rollDeg: 18,
      target: "topEdge",
      texture: "field",
      widthScale: 0.88,
    },
    {
      azimuthOffsetDeg: -82,
      color: [0, 0, 0],
      distanceScale: 0.72,
      elevationOffsetDeg: -4,
      heightScale: 1.18,
      id: "left-negative-fill-card",
      opacity: 0.9,
      rollDeg: 5,
      target: "leftEdge",
      texture: "soft",
      widthScale: 0.52,
    },
  ];
  const rectLightSpecs: readonly RectLightSpec[] = [
    {
      azimuthOffsetDeg: 0,
      color: "#ffffff",
      distanceScale: 1,
      elevationOffsetDeg: 0,
      heightScale: 1.08,
      id: "key-rect-light",
      intensity: direct * 8.4,
      rollDeg: -2,
      target: "frontCenter",
      widthScale: 0.2,
    },
    {
      azimuthOffsetDeg: 48,
      color: "#d8edff",
      distanceScale: 0.96,
      elevationOffsetDeg: 10,
      heightScale: 0.86,
      id: "cool-rim-rect-light",
      intensity: direct * 4.1,
      rollDeg: 4,
      target: "rightEdge",
      widthScale: 0.12,
    },
    {
      azimuthOffsetDeg: -38,
      color: "#ffb05b",
      distanceScale: 0.86,
      elevationOffsetDeg: -27,
      heightScale: 0.54,
      id: "warm-foot-rect-light",
      intensity: direct * (2.2 + settings.warm * 1.15),
      rollDeg: -12,
      target: "lowerLeft",
      widthScale: 0.24,
    },
  ];
  const pointLightSpecs: readonly PointLightSpec[] = [
    {
      azimuthOffsetDeg: -6,
      color: "#ffffff",
      distance: frame.radius * 2.8,
      distanceScale: 0.82,
      elevationOffsetDeg: -2,
      id: "front-specular-kick",
      intensity: direct * 2.6,
      target: "frontCenter",
    },
    {
      azimuthOffsetDeg: 42,
      color: "#b8dcff",
      distance: frame.radius * 2.45,
      distanceScale: 0.74,
      elevationOffsetDeg: 4,
      id: "right-edge-specular-kick",
      intensity: direct * 1.6,
      target: "rightEdge",
    },
    {
      azimuthOffsetDeg: -36,
      color: "#ff9d3b",
      distance: frame.radius * 2.2,
      distanceScale: 0.72,
      elevationOffsetDeg: -22,
      id: "warm-lower-specular-kick",
      intensity: direct * (1.2 + settings.warm * 0.8),
      target: "lowerLeft",
    },
  ];
  const probePosition = frame.center
    .clone()
    .add(frame.front.clone().multiplyScalar(Math.max(0.08, frame.depth * 0.2)));
  const eclipseSource = resolveEclipseSource(settings, frame);

  return {
    anchorDebug: logoAnchorIds.map((id) => ({
      color: debugAnchorColor(id),
      id,
      position: vectorToTuple(cloneLogoAnchor(frame, id)),
    })),
    cards: cardSpecs.map((spec) => resolveCard(settings, frame, spec)),
    eclipseSource,
    motionAmplitude: settings.rigMotion * 0.09,
    motionAxis: frame.up.clone(),
    origin: frame.center.clone(),
    pointLights: pointLightSpecs.map((spec) => resolvePointLight(settings, frame, spec)),
    probePosition: vectorToTuple(probePosition),
    rectLights: rectLightSpecs.map((spec) => resolveRectLight(settings, frame, spec)),
  };
}

export function guardianRigMotionQuaternion(
  rig: GuardianLightingRig,
  elapsedSeconds: number,
): Quaternion {
  return new Quaternion().setFromAxisAngle(
    rig.motionAxis,
    Math.sin(elapsedSeconds * 0.22) * rig.motionAmplitude,
  );
}

function resolveCard(
  settings: GuardianGlowSettings,
  frame: LogoFrame,
  spec: CardSpec,
): ResolvedReflectionCard {
  const targetWorld = cloneLogoAnchor(frame, spec.target);
  const positionWorld = resolveOrbitPosition(frame, {
    azimuthDeg: settings.keyAzimuth + spec.azimuthOffsetDeg,
    distanceScale: settings.keyDistance * spec.distanceScale,
    elevationDeg: settings.keyElevation + spec.elevationOffsetDeg,
    target: spec.target,
  });
  const position = positionWorld.clone().sub(frame.center);
  const target = targetWorld.clone().sub(frame.center);

  return {
    color: spec.color,
    id: spec.id,
    opacity: spec.opacity,
    position: vectorToTuple(position),
    rotation: rotationToTarget(position, target, spec.rollDeg),
    scale: [
      Math.max(0.05, frame.width * spec.widthScale * settings.cardSize),
      Math.max(0.05, frame.height * spec.heightScale * settings.cardSize),
      1,
    ],
    texture: spec.texture,
  };
}

function resolveRectLight(
  settings: GuardianGlowSettings,
  frame: LogoFrame,
  spec: RectLightSpec,
): ResolvedRectLight {
  const targetWorld = cloneLogoAnchor(frame, spec.target);
  const positionWorld = resolveOrbitPosition(frame, {
    azimuthDeg: settings.keyAzimuth + spec.azimuthOffsetDeg,
    distanceScale: settings.keyDistance * spec.distanceScale,
    elevationDeg: settings.keyElevation + spec.elevationOffsetDeg,
    target: spec.target,
  });
  const position = positionWorld.clone().sub(frame.center);
  const target = targetWorld.clone().sub(frame.center);

  return {
    color: spec.color,
    height: Math.max(0.05, frame.height * spec.heightScale * settings.cardSize),
    id: spec.id,
    intensity: spec.intensity,
    position: vectorToTuple(position),
    rotation: rotationToTarget(position, target, spec.rollDeg),
    width: Math.max(0.05, frame.width * spec.widthScale * settings.cardSize),
  };
}

function resolvePointLight(
  settings: GuardianGlowSettings,
  frame: LogoFrame,
  spec: PointLightSpec,
): ResolvedPointLight {
  const positionWorld = resolveOrbitPosition(frame, {
    azimuthDeg: settings.keyAzimuth + spec.azimuthOffsetDeg,
    distanceScale: settings.keyDistance * spec.distanceScale,
    elevationDeg: settings.keyElevation + spec.elevationOffsetDeg,
    target: spec.target,
  });

  return {
    color: spec.color,
    distance: spec.distance,
    id: spec.id,
    intensity: spec.intensity,
    position: vectorToTuple(positionWorld.clone().sub(frame.center)),
  };
}

function resolveEclipseSource(
  settings: GuardianGlowSettings,
  frame: LogoFrame,
): ResolvedEclipseSource {
  const positionWorld = frame.center
    .clone()
    .add(frame.back.clone().multiplyScalar(Math.max(0.16, frame.depth * 0.86)))
    .add(frame.right.clone().multiplyScalar(frame.width * -0.035))
    .add(frame.up.clone().multiplyScalar(frame.height * 0.045));
  const radius = frame.radius * settings.sourceRadius;
  const localPosition = positionWorld.clone().sub(frame.center);
  const localTarget = frame.center.clone().sub(frame.center);
  const warm = 0.9 + settings.warm * 0.12;

  return {
    color: [
      settings.sourceIntensity * 1.18 * warm,
      settings.sourceIntensity * 1.08 * warm,
      settings.sourceIntensity * 0.86,
    ],
    id: "eclipse-source",
    intensity: settings.sourceIntensity,
    opacity: Math.min(0.95, 0.42 + settings.sourceIntensity * 0.08),
    position: vectorToTuple(localPosition),
    radius,
    rectHeight: radius * 1.72,
    rectIntensity: settings.sourceIntensity * 4.8,
    rectWidth: radius * 1.72,
    rotation: rotationToTarget(localPosition, localTarget, 0),
  };
}

function resolveOrbitPosition(frame: LogoFrame, orbit: OrbitSpec) {
  const target = cloneLogoAnchor(frame, orbit.target);
  const azimuth = MathUtils.degToRad(orbit.azimuthDeg);
  const elevation = MathUtils.degToRad(orbit.elevationDeg);
  const horizontal = Math.cos(elevation);
  const distance = frame.radius * 0.58 * orbit.distanceScale;
  const offset = frame.front
    .clone()
    .multiplyScalar(Math.cos(azimuth) * horizontal * distance)
    .add(frame.right.clone().multiplyScalar(Math.sin(azimuth) * horizontal * distance))
    .add(frame.up.clone().multiplyScalar(Math.sin(elevation) * distance));

  return target.add(offset);
}

function rotationToTarget(
  position: Vector3,
  target: Vector3,
  rollDeg: number,
): readonly [number, number, number] {
  const object = new Object3D();
  object.position.copy(position);
  object.lookAt(target);
  object.rotateZ(MathUtils.degToRad(rollDeg));
  return [object.rotation.x, object.rotation.y, object.rotation.z];
}

function vectorToTuple(vector: Vector3): readonly [number, number, number] {
  return [vector.x, vector.y, vector.z];
}

function debugAnchorColor(anchor: LogoAnchorId): string {
  switch (anchor) {
    case "center":
    case "frontCenter":
    case "backCenter":
      return "#ffffff";
    case "frontUpperLeft":
    case "frontUpperRight":
    case "topEdge":
      return "#9dd7ff";
    case "bottomEdge":
    case "lowerLeft":
    case "lowerRight":
      return "#ffb35f";
    case "leftEdge":
    case "rightEdge":
      return "#b8ffdc";
  }
}
