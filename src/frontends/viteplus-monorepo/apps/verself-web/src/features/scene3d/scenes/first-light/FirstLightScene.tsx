import { animated, useSpring } from "@react-spring/three";
import { ContactShadows, Float } from "@react-three/drei";

const cubeRotation: [number, number, number] = [0.56, 0.74, 0.08];
const lightPosition: [number, number, number] = [-1.05, 1.16, 1.65];

export function FirstLightScene() {
  const reveal = useSpring({
    from: {
      cubeOpacity: 0,
      fillIntensity: 0,
      lightIntensity: 0,
      sourceScale: 0.18,
    },
    to: {
      cubeOpacity: 1,
      fillIntensity: 0.52,
      lightIntensity: 9.4,
      sourceScale: 1,
    },
    delay: 160,
    config: { mass: 1, tension: 54, friction: 22 },
  });

  return (
    <>
      <color attach="background" args={["#030303"]} />
      <fog attach="fog" args={["#050506", 5.5, 11]} />
      <group position={[0, -1.12, 0]} scale={0.5}>
        <ambientLight intensity={0.045} />
        <animated.pointLight
          color="#bcefff"
          decay={2}
          distance={6.4}
          intensity={reveal.lightIntensity}
          position={lightPosition}
        />
        <animated.spotLight
          angle={0.42}
          color="#fff4d4"
          decay={2}
          intensity={reveal.fillIntensity}
          penumbra={0.72}
          position={[1.4, 2.4, 2.6]}
        />
        <animated.mesh position={lightPosition} scale={reveal.sourceScale}>
          <sphereGeometry args={[0.08, 32, 32]} />
          <meshBasicMaterial color="#e9fbff" toneMapped={false} />
        </animated.mesh>
        <Float floatIntensity={0.18} rotationIntensity={0.08} speed={0.55}>
          <animated.mesh castShadow receiveShadow position={[0, -0.06, 0]} rotation={cubeRotation}>
            <boxGeometry args={[1.28, 1.28, 1.28]} />
            <animated.meshStandardMaterial
              color="#e8edf2"
              emissive="#8edaff"
              emissiveIntensity={0.08}
              metalness={0.1}
              opacity={reveal.cubeOpacity}
              roughness={0.44}
              transparent
            />
          </animated.mesh>
        </Float>
        <ContactShadows
          blur={2.4}
          color="#06111a"
          far={2.9}
          frames={1}
          opacity={0.28}
          position={[0, -1.08, 0]}
          scale={4.6}
        />
      </group>
    </>
  );
}
