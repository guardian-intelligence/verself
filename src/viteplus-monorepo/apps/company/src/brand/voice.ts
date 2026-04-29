// Machine-readable voice spec. Human version lives alongside at voice.md.
//
// The dynamic OG card generator calls assertVoice() on every string before
// PNG encode and refuses to emit a bad asset.

export type VoiceViolation =
  | {
      readonly kind: "banned_word";
      readonly word: string;
      readonly context: string;
    }
  | {
      readonly kind: "banned_hook";
      readonly pattern: string;
      readonly context: string;
    };

export type VoiceResult =
  | { readonly ok: true }
  | { readonly ok: false; readonly violations: readonly VoiceViolation[] };

// Use lower-case; match with word boundaries. "operator" is banned in
// user-facing copy but allowed in internal identifiers — the lint runs against
// src/content/** only, not src/routes/** or src/lib/**.
export const BANNED_WORDS: readonly string[] = [
  "empower",
  "unlock",
  "reimagine",
  "seamless",
  "cutting-edge",
  "game-changing",
  "industry-leading",
  "next-generation",
  "democratize",
  "disrupt",
  "journey",
  "passionate",
  "operator",
];

// "leverage" is banned as a verb but allowed as a noun ("operating leverage").
// The heuristic: "leverage" followed by a noun-phrase marker like "the", "a",
// "our", or an adjective+noun is verb-form. A single trailing word is also
// usually verb-form ("leverage AI"). False positives are loud-but-fixable; the
// alternative (missing verb usage) is worse.
const VERB_LEVERAGE = /\bleverage\s+(?:the|a|an|our|your|their|his|her|its|my|[a-z]+\b)/i;

// BuzzFeed hook regexes. Each matches the full "X, Y" closer so the reporter
// can show the whole offending clause. \b anchors prevent false positives on
// strings like "justine" or "notice".
const BANNED_HOOKS: ReadonlyArray<{ readonly name: string; readonly regex: RegExp }> = [
  { name: "it_s_not_just", regex: /\bit'?s\s+not\s+just\b[^.!?]*,[^.!?]*\bit'?s\b/i },
  { name: "that_s_not_an", regex: /\bthat'?s\s+not\s+(?:a|an|the)\b[^.!?]*,[^.!?]*\bthat'?s\b/i },
  { name: "it_s_more_than", regex: /\bit'?s\s+more\s+than\b[^.!?]*,[^.!?]*\bit'?s\b/i },
  {
    name: "not_x_but_y",
    regex: /\bnot\s+(?:a|an|the)\s+[a-z]+[^.!?]*,\s*but\s+(?:a|an|the)\s+[a-z]+/i,
  },
];

export function assertVoice(input: string, contextLabel = ""): VoiceResult {
  const violations: VoiceViolation[] = [];
  const lowered = input.toLowerCase();

  for (const word of BANNED_WORDS) {
    // Word boundaries. Hyphens count as boundaries in JS \b, which is why
    // "cutting-edge" needs the raw substring match rather than \b…\b.
    if (word.includes("-")) {
      if (lowered.includes(word)) {
        violations.push({ kind: "banned_word", word, context: contextLabel });
      }
      continue;
    }
    const regex = new RegExp(`\\b${word}\\b`, "i");
    if (regex.test(lowered)) {
      violations.push({ kind: "banned_word", word, context: contextLabel });
    }
  }

  if (VERB_LEVERAGE.test(input)) {
    violations.push({ kind: "banned_word", word: "leverage (as verb)", context: contextLabel });
  }

  for (const hook of BANNED_HOOKS) {
    if (hook.regex.test(input)) {
      violations.push({ kind: "banned_hook", pattern: hook.name, context: contextLabel });
    }
  }

  return violations.length === 0 ? { ok: true } : { ok: false, violations };
}

export function formatViolation(violation: VoiceViolation): string {
  switch (violation.kind) {
    case "banned_word":
      return `banned word "${violation.word}"${violation.context ? ` in ${violation.context}` : ""}`;
    case "banned_hook":
      return `banned hook pattern "${violation.pattern}"${violation.context ? ` in ${violation.context}` : ""}`;
  }
}
