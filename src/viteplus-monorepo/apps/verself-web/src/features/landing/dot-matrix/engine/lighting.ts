import { Color, Vector3 } from "three";

export const DOT_MATRIX_LIGHTING = {
  ambient: 0.28,
  exposure: 1.04,
  keyColor: new Color(0xffffff),
  keyDirection: new Vector3(-0.34, 0.52, 0.78).normalize(),
  rimColor: new Color(0x7edbd2),
  rimDirection: new Vector3(0.62, -0.18, 0.76).normalize(),
  surfaceDepth: 9.5,
} as const;
