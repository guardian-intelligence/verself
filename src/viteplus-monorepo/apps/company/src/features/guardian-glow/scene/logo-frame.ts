import { Euler, Matrix4, Quaternion, Vector3 } from "three";
import { getGuardianLogoLocalBounds } from "./GuardianLogoGeometry";

export const GUARDIAN_LOGO_POSITION = [-0.62, -0.06, 0] as const;
export const GUARDIAN_LOGO_ROTATION = [0.24, -0.16, -0.08] as const;

export const logoAnchorIds = [
  "center",
  "frontCenter",
  "backCenter",
  "frontUpperRight",
  "frontUpperLeft",
  "rightEdge",
  "leftEdge",
  "topEdge",
  "bottomEdge",
  "lowerLeft",
  "lowerRight",
] as const;

export type LogoAnchorId = (typeof logoAnchorIds)[number];

export type LogoFrame = {
  readonly anchors: Readonly<Record<LogoAnchorId, Vector3>>;
  readonly back: Vector3;
  readonly center: Vector3;
  readonly depth: number;
  readonly front: Vector3;
  readonly height: number;
  readonly radius: number;
  readonly right: Vector3;
  readonly up: Vector3;
  readonly width: number;
};

export function createGuardianLogoFrame(): LogoFrame {
  const bounds = getGuardianLogoLocalBounds();
  const localSize = bounds.getSize(new Vector3());
  const localCenter = bounds.getCenter(new Vector3());
  const localFrontZ = bounds.max.z;
  const localBackZ = bounds.min.z;
  const position = new Vector3(...GUARDIAN_LOGO_POSITION);
  const rotation = new Euler(...GUARDIAN_LOGO_ROTATION);
  const orientation = new Quaternion().setFromEuler(rotation);
  const worldMatrix = new Matrix4().compose(position, orientation, new Vector3(1, 1, 1));
  const transformPoint = (point: Vector3) => point.clone().applyMatrix4(worldMatrix);
  const center = transformPoint(localCenter);
  const anchors: Readonly<Record<LogoAnchorId, Vector3>> = {
    backCenter: transformPoint(new Vector3(localCenter.x, localCenter.y, localBackZ)),
    bottomEdge: transformPoint(new Vector3(localCenter.x, bounds.min.y, localFrontZ)),
    center,
    frontCenter: transformPoint(new Vector3(localCenter.x, localCenter.y, localFrontZ)),
    frontUpperLeft: transformPoint(new Vector3(bounds.min.x, bounds.max.y, localFrontZ)),
    frontUpperRight: transformPoint(new Vector3(bounds.max.x, bounds.max.y, localFrontZ)),
    leftEdge: transformPoint(new Vector3(bounds.min.x, localCenter.y, localFrontZ)),
    lowerLeft: transformPoint(new Vector3(bounds.min.x, bounds.min.y, localFrontZ)),
    lowerRight: transformPoint(new Vector3(bounds.max.x, bounds.min.y, localFrontZ)),
    rightEdge: transformPoint(new Vector3(bounds.max.x, localCenter.y, localFrontZ)),
    topEdge: transformPoint(new Vector3(localCenter.x, bounds.max.y, localFrontZ)),
  };

  return {
    anchors,
    back: new Vector3(0, 0, -1).applyQuaternion(orientation).normalize(),
    center,
    depth: localSize.z,
    front: new Vector3(0, 0, 1).applyQuaternion(orientation).normalize(),
    height: localSize.y,
    radius: localSize.length() * 0.5,
    right: new Vector3(1, 0, 0).applyQuaternion(orientation).normalize(),
    up: new Vector3(0, 1, 0).applyQuaternion(orientation).normalize(),
    width: localSize.x,
  };
}

export function cloneLogoAnchor(frame: LogoFrame, anchor: LogoAnchorId): Vector3 {
  return frame.anchors[anchor].clone();
}
