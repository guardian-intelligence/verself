import { LandingSpotlight } from "~/features/scene3d/LandingSpotlight";
import { useNavee } from "./navee-context";
import { NaveeMatrixScene } from "./navee-matrix-scene";

export function NaveeStage() {
  const { snapshot } = useNavee();
  if (snapshot.aperture === "hidden") {
    return (
      <div
        aria-hidden="true"
        className="navee-stage"
        data-navee-aperture={snapshot.aperture}
        data-navee-mode={snapshot.mode}
      />
    );
  }

  return (
    <div
      aria-hidden="true"
      className="navee-stage"
      data-navee-aperture={snapshot.aperture}
      data-navee-mode={snapshot.mode}
    >
      <div className="navee-aperture">
        <NaveeMatrixScene className="opacity-95" />
        <LandingSpotlight className="absolute inset-0" />
      </div>
    </div>
  );
}
