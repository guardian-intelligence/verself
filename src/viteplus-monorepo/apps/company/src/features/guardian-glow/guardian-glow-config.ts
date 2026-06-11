export const guardianGlowStages = [
  "beauty",
  "cards",
  "probe",
  "reflections",
  "eclipse",
  "highlight",
  "rays",
  "bloom",
  "aberration",
  "composite",
] as const;

export type GuardianGlowStage = (typeof guardianGlowStages)[number];

export const guardianGlowProfiles = [
  "no-msaa",
  "full",
  "raw-scene",
  "probe-low",
  "probe-high",
] as const;

export type GuardianGlowProfile = (typeof guardianGlowProfiles)[number];

export type GuardianGlowDialGroup = "material" | "lighting" | "eclipse" | "probe" | "post";

export type GuardianGlowDialKey =
  | "threshold"
  | "knee"
  | "gradient"
  | "blur"
  | "bloom"
  | "aberration"
  | "keyAzimuth"
  | "keyElevation"
  | "keyDistance"
  | "cardSize"
  | "rigMotion"
  | "warm"
  | "lightIntensity"
  | "cardIntensity"
  | "sourceIntensity"
  | "sourceRadius"
  | "rayIntensity"
  | "rayLength"
  | "probeCadence"
  | "metalness"
  | "roughness"
  | "clearcoat"
  | "env"
  | "exposure";

export type GuardianGlowSettings = {
  readonly stage: GuardianGlowStage;
  readonly profile: GuardianGlowProfile;
  readonly debug?: "fps";
  readonly developmentMode?: "1";
  readonly aberration: number;
  readonly bloom: number;
  readonly blur: number;
  readonly cardIntensity: number;
  readonly cardSize: number;
  readonly clearcoat: number;
  readonly env: number;
  readonly exposure: number;
  readonly gradient: number;
  readonly keyAzimuth: number;
  readonly keyDistance: number;
  readonly keyElevation: number;
  readonly knee: number;
  readonly lightIntensity: number;
  readonly metalness: number;
  readonly probeCadence: number;
  readonly rayIntensity: number;
  readonly rayLength: number;
  readonly rigMotion: number;
  readonly roughness: number;
  readonly sourceIntensity: number;
  readonly sourceRadius: number;
  readonly threshold: number;
  readonly warm: number;
};

export type GuardianGlowDial = {
  readonly group: GuardianGlowDialGroup;
  readonly key: GuardianGlowDialKey;
  readonly label: string;
  readonly min: number;
  readonly max: number;
  readonly step: number;
};

export const guardianGlowDefaults: GuardianGlowSettings = {
  stage: "composite",
  profile: "no-msaa",
  threshold: 0.38,
  knee: 0.18,
  gradient: 0.12,
  blur: 14.5,
  bloom: 3.0,
  aberration: 7.2,
  keyAzimuth: -8,
  keyElevation: 14,
  keyDistance: 2.25,
  cardSize: 1,
  rigMotion: 0.65,
  warm: 0.62,
  lightIntensity: 1.2,
  cardIntensity: 1.6,
  sourceIntensity: 2.4,
  sourceRadius: 1.42,
  rayIntensity: 1.35,
  rayLength: 0.82,
  probeCadence: 6,
  metalness: 0.62,
  roughness: 0.32,
  clearcoat: 0.92,
  env: 1.75,
  exposure: 1.08,
};

export const guardianGlowDials: readonly GuardianGlowDial[] = [
  { group: "post", key: "threshold", label: "Threshold", min: 0.35, max: 1.8, step: 0.01 },
  { group: "post", key: "knee", label: "Knee", min: 0.02, max: 0.7, step: 0.01 },
  { group: "post", key: "gradient", label: "Gradient gate", min: 0.02, max: 0.8, step: 0.01 },
  { group: "post", key: "blur", label: "Blur radius", min: 1, max: 24, step: 0.25 },
  { group: "post", key: "bloom", label: "Bloom gain", min: 0, max: 4, step: 0.05 },
  { group: "post", key: "aberration", label: "Channel offset", min: 0, max: 14, step: 0.1 },
  { group: "post", key: "exposure", label: "Exposure", min: 0.55, max: 1.45, step: 0.01 },
  { group: "lighting", key: "keyAzimuth", label: "Key azimuth", min: -70, max: 70, step: 1 },
  { group: "lighting", key: "keyElevation", label: "Key elevation", min: -35, max: 55, step: 1 },
  {
    group: "lighting",
    key: "keyDistance",
    label: "Key distance",
    min: 1.25,
    max: 3.75,
    step: 0.05,
  },
  { group: "lighting", key: "cardSize", label: "Card size", min: 0.45, max: 1.8, step: 0.01 },
  { group: "lighting", key: "rigMotion", label: "Rig motion", min: 0, max: 1.5, step: 0.01 },
  { group: "lighting", key: "warm", label: "Warm glint", min: 0, max: 1.6, step: 0.02 },
  {
    group: "lighting",
    key: "lightIntensity",
    label: "Direct light",
    min: 0,
    max: 2.2,
    step: 0.02,
  },
  {
    group: "lighting",
    key: "cardIntensity",
    label: "Card intensity",
    min: 0,
    max: 3,
    step: 0.02,
  },
  {
    group: "eclipse",
    key: "sourceIntensity",
    label: "Source energy",
    min: 0,
    max: 6,
    step: 0.05,
  },
  {
    group: "eclipse",
    key: "sourceRadius",
    label: "Source radius",
    min: 0.65,
    max: 2.8,
    step: 0.01,
  },
  {
    group: "eclipse",
    key: "rayIntensity",
    label: "Ray gain",
    min: 0,
    max: 5,
    step: 0.05,
  },
  {
    group: "eclipse",
    key: "rayLength",
    label: "Ray length",
    min: 0.15,
    max: 1.8,
    step: 0.01,
  },
  { group: "probe", key: "probeCadence", label: "Probe cadence", min: 1, max: 24, step: 1 },
  { group: "material", key: "metalness", label: "Metalness", min: 0, max: 1, step: 0.01 },
  { group: "material", key: "roughness", label: "Roughness", min: 0.02, max: 0.7, step: 0.01 },
  { group: "material", key: "clearcoat", label: "Clearcoat", min: 0, max: 1, step: 0.01 },
  { group: "material", key: "env", label: "Reflection gain", min: 0, max: 3, step: 0.02 },
] as const;

export type GuardianGlowRuntime = {
  readonly cardDebugVisible: boolean;
  readonly composer: boolean;
  readonly composerResolutionScale: number;
  readonly directLights: boolean;
  readonly dpr: [number, number];
  readonly dynamicProbe: boolean;
  readonly logoVisible: boolean;
  readonly multisampling: number;
  readonly probeFrameInterval: number;
  readonly probeResolution: number;
};

export function parseGuardianGlowSearch(search: Record<string, unknown>): GuardianGlowSettings {
  const settings: GuardianGlowSettings = {
    stage: parseStage(search.stage),
    profile: parseProfile(search.profile),
    aberration: parseDial(search.aberration, "aberration"),
    bloom: parseDial(search.bloom, "bloom"),
    blur: parseDial(search.blur, "blur"),
    cardIntensity: parseDial(search.cardIntensity, "cardIntensity"),
    cardSize: parseDial(search.cardSize, "cardSize"),
    clearcoat: parseDial(search.clearcoat, "clearcoat"),
    env: parseDial(search.env, "env"),
    exposure: parseDial(search.exposure, "exposure"),
    gradient: parseDial(search.gradient, "gradient"),
    keyAzimuth: parseDial(search.keyAzimuth, "keyAzimuth"),
    keyDistance: parseDial(search.keyDistance, "keyDistance"),
    keyElevation: parseDial(search.keyElevation, "keyElevation"),
    knee: parseDial(search.knee, "knee"),
    lightIntensity: parseDial(search.lightIntensity, "lightIntensity"),
    metalness: parseDial(search.metalness, "metalness"),
    probeCadence: parseDial(search.probeCadence, "probeCadence"),
    rayIntensity: parseDial(search.rayIntensity, "rayIntensity"),
    rayLength: parseDial(search.rayLength, "rayLength"),
    rigMotion: parseDial(search.rigMotion, "rigMotion"),
    roughness: parseDial(search.roughness, "roughness"),
    sourceIntensity: parseDial(search.sourceIntensity, "sourceIntensity"),
    sourceRadius: parseDial(search.sourceRadius, "sourceRadius"),
    threshold: parseDial(search.threshold, "threshold"),
    warm: parseDial(search.warm, "warm"),
  };

  return {
    ...settings,
    ...(search.debug === "fps" ? { debug: "fps" as const } : {}),
    ...(isDevelopmentModeSearchValue(search.developmentMode)
      ? { developmentMode: "1" as const }
      : {}),
  };
}

export function guardianGlowStageCode(stage: GuardianGlowStage): number {
  return guardianGlowStages.indexOf(stage);
}

export function resolveGuardianGlowRuntime(settings: GuardianGlowSettings): GuardianGlowRuntime {
  const dynamicProbe = settings.stage !== "beauty";
  const composer =
    settings.profile !== "raw-scene" &&
    (settings.stage === "highlight" ||
      settings.stage === "rays" ||
      settings.stage === "bloom" ||
      settings.stage === "aberration" ||
      settings.stage === "composite");

  return {
    cardDebugVisible: settings.stage === "cards",
    composer,
    composerResolutionScale: composerResolutionScale(settings.profile),
    directLights: true,
    dpr: settings.profile === "full" ? [1.5, 2] : [1, 1],
    dynamicProbe,
    logoVisible: true,
    multisampling: settings.profile === "full" ? 4 : 0,
    probeFrameInterval: Math.max(
      1,
      Math.round(settings.probeCadence * probeCadenceMultiplier(settings.profile)),
    ),
    probeResolution: probeResolution(settings.profile),
  };
}

function parseStage(value: unknown): GuardianGlowStage {
  return guardianGlowStages.includes(value as GuardianGlowStage)
    ? (value as GuardianGlowStage)
    : guardianGlowDefaults.stage;
}

function parseProfile(value: unknown): GuardianGlowProfile {
  return guardianGlowProfiles.includes(value as GuardianGlowProfile)
    ? (value as GuardianGlowProfile)
    : guardianGlowDefaults.profile;
}

function isDevelopmentModeSearchValue(value: unknown): boolean {
  return value === "1" || value === 1 || value === '"1"';
}

function parseDial(value: unknown, key: GuardianGlowDialKey): number {
  const dial = guardianGlowDials.find((candidate) => candidate.key === key);
  const parsed =
    typeof value === "number"
      ? value
      : typeof value === "string" && value.trim()
        ? Number.parseFloat(value)
        : Number.NaN;

  if (!dial || Number.isNaN(parsed)) {
    return guardianGlowDefaults[key];
  }

  return Math.min(dial.max, Math.max(dial.min, parsed));
}

function probeResolution(profile: GuardianGlowProfile): number {
  switch (profile) {
    case "full":
    case "probe-high":
      return 256;
    case "probe-low":
      return 64;
    case "no-msaa":
    case "raw-scene":
      return 128;
  }
}

function probeCadenceMultiplier(profile: GuardianGlowProfile): number {
  switch (profile) {
    case "probe-high":
      return 0.33;
    case "full":
      return 0.5;
    case "probe-low":
      return 2;
    case "no-msaa":
    case "raw-scene":
      return 1;
  }
}

function composerResolutionScale(profile: GuardianGlowProfile): number {
  switch (profile) {
    case "full":
      return 1;
    case "probe-high":
      return 0.9;
    case "no-msaa":
      return 0.72;
    case "probe-low":
      return 0.62;
    case "raw-scene":
      return 1;
  }
}
