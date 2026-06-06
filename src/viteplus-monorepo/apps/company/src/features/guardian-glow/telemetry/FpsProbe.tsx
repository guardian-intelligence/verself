import { useFrame } from "@react-three/fiber";
import { useRef } from "react";
import { guardianGlowMetricIds, setGuardianGlowMetric } from "./metrics";

const SAMPLE_WINDOW_MS = 350;

export function FpsProbe() {
  const sampleRef = useRef({ frames: 0, ms: 0 });

  useFrame(({ gl, size }, delta) => {
    const sample = sampleRef.current;
    sample.frames += 1;
    sample.ms += delta * 1000;

    if (sample.ms < SAMPLE_WINDOW_MS) {
      return;
    }

    const fps = (sample.frames * 1000) / sample.ms;
    const frameMs = sample.ms / sample.frames;
    const dpr = gl.getPixelRatio();
    const pixels = size.width * size.height * dpr * dpr;

    setGuardianGlowMetric(guardianGlowMetricIds.fps, Math.round(fps).toString());
    setGuardianGlowMetric(guardianGlowMetricIds.frame, frameMs.toFixed(1));
    setGuardianGlowMetric(guardianGlowMetricIds.dpr, dpr.toFixed(2));
    setGuardianGlowMetric(guardianGlowMetricIds.pixels, `${(pixels / 1_000_000).toFixed(2)}M`);
    setGuardianGlowMetric(guardianGlowMetricIds.draws, gl.info.render.calls.toString());
    setGuardianGlowMetric(
      guardianGlowMetricIds.triangles,
      gl.info.render.triangles.toLocaleString(),
    );
    setGuardianGlowMetric(
      guardianGlowMetricIds.programs,
      (gl.info.programs?.length ?? 0).toString(),
    );
    sample.frames = 0;
    sample.ms = 0;
  });

  return null;
}
