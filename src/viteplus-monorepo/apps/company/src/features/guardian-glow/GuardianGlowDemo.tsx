import { lazy, type ChangeEvent } from "react";
import { ClientOnly, useNavigate } from "@tanstack/react-router";
import { Suspense } from "react";
import {
  guardianGlowDials,
  guardianGlowProfiles,
  guardianGlowStages,
  type GuardianGlowDial,
  type GuardianGlowDialGroup,
  type GuardianGlowProfile,
  type GuardianGlowSettings,
  type GuardianGlowStage,
} from "./guardian-glow-config";

const GuardianGlowCanvas = lazy(() =>
  import("./scene/GuardianGlowCanvas").then((module) => ({ default: module.GuardianGlowCanvas })),
);

type GuardianGlowDemoProps = {
  readonly settings: GuardianGlowSettings;
};

export function GuardianGlowDemo({ settings }: GuardianGlowDemoProps) {
  const navigate = useNavigate();
  const showDebugPanel = settings.developmentMode === "1";
  const dialGroups: readonly GuardianGlowDialGroup[] = ["lighting", "probe", "material", "post"];

  const updateDial = (dial: GuardianGlowDial) => async (event: ChangeEvent<HTMLInputElement>) => {
    await navigate({
      replace: true,
      search: { ...settings, [dial.key]: Number.parseFloat(event.currentTarget.value) },
      to: "/",
    });
  };

  const updateStage = async (event: ChangeEvent<HTMLSelectElement>) => {
    await navigate({
      replace: true,
      search: { ...settings, stage: event.currentTarget.value as GuardianGlowStage },
      to: "/",
    });
  };

  const updateProfile = async (event: ChangeEvent<HTMLSelectElement>) => {
    await navigate({
      replace: true,
      search: { ...settings, profile: event.currentTarget.value as GuardianGlowProfile },
      to: "/",
    });
  };

  return (
    <main className="relative min-h-svh overflow-hidden bg-black text-white">
      <div aria-hidden="true" className="absolute inset-0">
        <ClientOnly fallback={<GuardianGlowFallback />}>
          <Suspense fallback={<GuardianGlowFallback />}>
            <GuardianGlowCanvas settings={settings} />
          </Suspense>
        </ClientOnly>
      </div>

      {showDebugPanel ? (
        <section className="absolute inset-x-0 bottom-0 max-h-[88svh] overflow-auto border-t border-white/10 bg-black/72 px-4 py-3 backdrop-blur md:inset-x-auto md:bottom-5 md:right-5 md:w-[390px] md:border md:p-4">
          <div className="grid gap-3">
            <div className="grid grid-cols-2 border border-white/12 bg-white/[0.03] font-mono text-[11px] uppercase tracking-[0.16em] text-white/62">
              <div className="border-r border-white/10 px-3 py-2">
                <div>FPS</div>
                <div
                  id="guardian-glow-fps-value"
                  className="mt-1 text-lg tracking-normal text-white"
                >
                  --
                </div>
              </div>
              <div className="px-3 py-2">
                <div>Frame MS</div>
                <div
                  id="guardian-glow-frame-value"
                  className="mt-1 text-lg tracking-normal text-white"
                >
                  --
                </div>
              </div>
            </div>

            <div className="grid grid-cols-5 gap-px border border-white/12 bg-white/10 font-mono text-[10px] uppercase tracking-[0.14em] text-white/54">
              <MetricCell id="guardian-glow-dpr-value" label="DPR" />
              <MetricCell id="guardian-glow-pixels-value" label="Pixels" />
              <MetricCell id="guardian-glow-draws-value" label="Draws" />
              <MetricCell id="guardian-glow-triangles-value" label="Tris" />
              <MetricCell id="guardian-glow-programs-value" label="Programs" />
            </div>

            <div className="grid grid-cols-4 gap-px border border-white/12 bg-white/10 font-mono text-[10px] uppercase tracking-[0.14em] text-white/54">
              <MetricCell id="guardian-glow-probe-res-value" label="Probe" />
              <MetricCell id="guardian-glow-probe-rate-value" label="Cadence" />
              <MetricCell id="guardian-glow-probe-ms-value" label="Probe MS" />
              <MetricCell id="guardian-glow-probe-count-value" label="Updates" />
            </div>

            <div className="grid gap-2 md:grid-cols-2">
              <label className="grid gap-1 text-xs uppercase tracking-[0.18em] text-white/58">
                Stage
                <select
                  className="h-9 border border-white/14 bg-black px-2 text-sm normal-case tracking-normal text-white outline-none"
                  onChange={updateStage}
                  value={settings.stage}
                >
                  {guardianGlowStages.map((stage) => (
                    <option key={stage} value={stage}>
                      {stage}
                    </option>
                  ))}
                </select>
              </label>

              <label className="grid gap-1 text-xs uppercase tracking-[0.18em] text-white/58">
                Profile
                <select
                  className="h-9 border border-white/14 bg-black px-2 text-sm normal-case tracking-normal text-white outline-none"
                  onChange={updateProfile}
                  value={settings.profile}
                >
                  {guardianGlowProfiles.map((profile) => (
                    <option key={profile} value={profile}>
                      {profile}
                    </option>
                  ))}
                </select>
              </label>
            </div>

            <div className="grid gap-2">
              {dialGroups.map((group) => (
                <fieldset key={group} className="grid gap-2 border border-white/10 px-3 py-2">
                  <legend className="px-1 text-[10px] uppercase tracking-[0.18em] text-white/42">
                    {group}
                  </legend>
                  {guardianGlowDials
                    .filter((dial) => dial.group === group)
                    .map((dial) => (
                      <label
                        key={dial.key}
                        className="grid grid-cols-[minmax(0,1fr)_4.5rem] items-center gap-x-3 gap-y-1 text-xs uppercase tracking-[0.18em] text-white/58"
                      >
                        <span>{dial.label}</span>
                        <span className="text-right font-mono text-[11px] tracking-normal text-white/78">
                          {settings[dial.key].toFixed(dial.step < 0.1 ? 2 : 0)}
                        </span>
                        <input
                          className="col-span-2 h-5 accent-white"
                          max={dial.max}
                          min={dial.min}
                          onChange={updateDial(dial)}
                          step={dial.step}
                          type="range"
                          value={settings[dial.key]}
                        />
                      </label>
                    ))}
                </fieldset>
              ))}
            </div>
          </div>
        </section>
      ) : (
        <HiddenMetricSink />
      )}
    </main>
  );
}

function MetricCell({ id, label }: { readonly id: string; readonly label: string }) {
  return (
    <div className="bg-black/84 px-2 py-1.5">
      <div>{label}</div>
      <div id={id} className="mt-0.5 tracking-normal text-white/88">
        --
      </div>
    </div>
  );
}

function HiddenMetricSink() {
  return (
    <div hidden>
      <span id="guardian-glow-fps-value" />
      <span id="guardian-glow-frame-value" />
      <span id="guardian-glow-dpr-value" />
      <span id="guardian-glow-pixels-value" />
      <span id="guardian-glow-draws-value" />
      <span id="guardian-glow-triangles-value" />
      <span id="guardian-glow-programs-value" />
      <span id="guardian-glow-probe-res-value" />
      <span id="guardian-glow-probe-rate-value" />
      <span id="guardian-glow-probe-ms-value" />
      <span id="guardian-glow-probe-count-value" />
    </div>
  );
}

function GuardianGlowFallback() {
  return (
    <div
      className="absolute inset-0 bg-black"
      style={{
        background: "radial-gradient(circle at 50% 48%, #101010 0%, #050505 42%, #000000 72%)",
      }}
    />
  );
}
