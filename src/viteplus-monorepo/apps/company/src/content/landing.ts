// Landing copy lives in content/, not in JSX. The route is structural —
// components reference `landing.hero`, `landing.mission[0]`, etc. — so a forker
// rewrites this file with their coding agent and the routes keep working.
//
// Voice rules in brand/voice.md apply. Phase 3 wires a lint that asserts every
// string in this module passes assertVoice() before build.

export const landing = {
  kicker: "Guardian Intelligence · An American applied intelligence company · Seattle, Washington",
  hero: "The world needs your business to succeed. We're here to help.",
  mission: [
    "Every founder spends the first year on the same dozen systems — identity, billing, analytics, email, infrastructure, security, the thousand edges where a real company touches the real world. None of it is what you started the company to build. We build the reference architecture for all of it — open-source, documented, and clean enough that one founder with Claude Code can run a billion-dollar company.",
    "Value created per capita is the ultimate metric. A painting. A novel. An API in front of a physical service. A quiet service that sends a calendar invite to the neighborhood when the dog park is going to be 72 and sunny with 80% confidence. Humanity's golden age is the one where every person contributes unprecedented value to the world, and software and AI finally make that possible for everyone.",
  ],
  closer: "If you want to do something good for the world, we want to make it easy.",
} as const;

export type LandingContent = typeof landing;
