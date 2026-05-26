import { useFrame } from "@react-three/fiber";
import { useRef } from "react";
import { MathUtils, type Camera, type Group, type Mesh, PerspectiveCamera } from "three";
import { firstLightRoom } from "./first-light-config";
import { useSceneRuntime } from "../../scene-runtime";

function isPerspectiveCamera(camera: Camera): camera is PerspectiveCamera {
  return camera instanceof PerspectiveCamera;
}

export function ViewportRoom() {
  const runtime = useSceneRuntime();
  const room = useRef<Group>(null);
  const backWall = useRef<Mesh>(null);
  const floor = useRef<Mesh>(null);
  const ceiling = useRef<Mesh>(null);
  const leftWall = useRef<Mesh>(null);
  const rightWall = useRef<Mesh>(null);

  useFrame(({ camera, size }) => {
    if (!room.current || !isPerspectiveCamera(camera)) {
      return;
    }

    const roomDepth = MathUtils.clamp(
      runtime.camera.focusDistance + firstLightRoom.focusDepthPadding,
      firstLightRoom.minDepth,
      firstLightRoom.maxDepth,
    );
    const wallDepth = roomDepth - firstLightRoom.frontDepth;
    const aspect = size.width / size.height;
    const viewportHeight = 2 * Math.tan(MathUtils.degToRad(camera.fov) / 2) * roomDepth;
    const viewportWidth = viewportHeight * aspect;
    const roomWidth = viewportWidth * firstLightRoom.widthScale;
    const roomHeight = viewportHeight * firstLightRoom.heightScale;
    const centerY = firstLightRoom.verticalOffset;
    const wallCenterZ = -(roomDepth + firstLightRoom.frontDepth) / 2;

    room.current.position.copy(camera.position);
    room.current.quaternion.copy(camera.quaternion);

    if (backWall.current) {
      backWall.current.position.set(0, centerY, -roomDepth);
      backWall.current.scale.set(roomWidth, roomHeight, 1);
    }

    if (floor.current) {
      floor.current.position.set(0, centerY - roomHeight / 2, wallCenterZ);
      floor.current.scale.set(roomWidth, wallDepth, 1);
    }

    if (ceiling.current) {
      ceiling.current.position.set(0, centerY + roomHeight / 2, wallCenterZ);
      ceiling.current.scale.set(roomWidth, wallDepth, 1);
    }

    if (leftWall.current) {
      leftWall.current.position.set(-roomWidth / 2, centerY, wallCenterZ);
      leftWall.current.scale.set(wallDepth, roomHeight, 1);
    }

    if (rightWall.current) {
      rightWall.current.position.set(roomWidth / 2, centerY, wallCenterZ);
      rightWall.current.scale.set(wallDepth, roomHeight, 1);
    }
  });

  return (
    <group ref={room}>
      <mesh ref={backWall} receiveShadow>
        <planeGeometry args={[1, 1]} />
        <meshStandardMaterial
          color="#211a15"
          emissive="#080605"
          emissiveIntensity={0.1}
          metalness={0}
          roughness={0.96}
        />
      </mesh>
      <mesh ref={floor} receiveShadow rotation={[-Math.PI / 2, 0, 0]}>
        <planeGeometry args={[1, 1]} />
        <meshStandardMaterial
          color="#281a10"
          emissive="#090504"
          emissiveIntensity={0.08}
          metalness={0}
          roughness={0.98}
        />
      </mesh>
      <mesh ref={ceiling} receiveShadow rotation={[Math.PI / 2, 0, 0]}>
        <planeGeometry args={[1, 1]} />
        <meshStandardMaterial
          color="#11161a"
          emissive="#040506"
          emissiveIntensity={0.12}
          metalness={0}
          roughness={0.98}
        />
      </mesh>
      <mesh ref={leftWall} receiveShadow rotation={[0, Math.PI / 2, 0]}>
        <planeGeometry args={[1, 1]} />
        <meshStandardMaterial
          color="#181514"
          emissive="#050404"
          emissiveIntensity={0.1}
          metalness={0}
          roughness={0.97}
        />
      </mesh>
      <mesh ref={rightWall} receiveShadow rotation={[0, -Math.PI / 2, 0]}>
        <planeGeometry args={[1, 1]} />
        <meshStandardMaterial
          color="#221812"
          emissive="#070503"
          emissiveIntensity={0.1}
          metalness={0}
          roughness={0.97}
        />
      </mesh>
    </group>
  );
}
