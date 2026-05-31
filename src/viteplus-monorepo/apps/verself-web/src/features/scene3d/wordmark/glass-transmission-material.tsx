import { MeshTransmissionMaterial, type MeshTransmissionMaterialProps } from "@react-three/drei";
import { wordmarkTransmission } from "./wordmark-config";

type GlassTransmissionMaterialProps = Omit<MeshTransmissionMaterialProps, "ref">;

export function GlassTransmissionMaterial(props: GlassTransmissionMaterialProps) {
  return <MeshTransmissionMaterial {...wordmarkTransmission} {...props} />;
}
