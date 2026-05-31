import { HeroWordmarkScene } from "../hero-wordmark/HeroWordmarkScene";

export function WordmarkLabScene() {
  return (
    <>
      <color attach="background" args={["#030304"]} />
      <fog attach="fog" args={["#050506", 4.5, 14]} />
      <HeroWordmarkScene screenWidthBasis="viewport" />
    </>
  );
}
