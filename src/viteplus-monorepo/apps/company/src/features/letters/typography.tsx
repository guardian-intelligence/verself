import type { Letter } from "~/content/letters";
import { lettersSignatureFont } from "~/features/letters/fonts";
import { syncLetterContinuationMetrics } from "~/features/letters/transitions.intent";
import { transitionStyle } from "~/features/letters/transitions";

export const LETTER_READING_COLUMN_CLASS = "mx-auto w-full max-w-6xl px-[var(--chrome-inline-gap)]";

export const LETTER_TEXT_MEASURE_CLASS = "max-w-[46rem]";

export const LETTER_INDEX_PAGE_PADDING_CLASS =
  "pb-24 pt-14 sm:pb-28 sm:pt-[72px] md:pb-32 md:pt-20";

export const LETTER_POST_PAGE_PADDING_CLASS = "pb-24 pt-4 sm:pb-28 sm:pt-5 md:pb-32 md:pt-6";

// The date is set in the signature hand — the same cursive that signs the
// letter — so the sheet opens and closes in the writer's own script.
const LETTER_DATE_CLASS = "text-[var(--treatment-ink)]";

const LETTER_SALUTATION_CLASS =
  "font-display font-normal text-[var(--treatment-ink)] [font-variation-settings:'opsz'_18,'SOFT'_0]";

const LETTER_BODY_CLASS =
  "font-display font-normal text-[var(--treatment-muted-strong)] [font-variation-settings:'opsz'_18,'SOFT'_0] text-[18px] leading-[1.62] md:text-[clamp(19px,1.4vw,20px)]";

export const letterProseClassName = [
  "w-full",
  LETTER_BODY_CLASS,
  "[overflow-wrap:break-word]",
  "[&>*+*]:mt-7",
  "[&>p]:text-[18px] [&>p]:font-normal [&>p]:leading-[1.62] md:[&>p]:text-[clamp(19px,1.4vw,20px)]",
  "[&>blockquote]:border-l-2 [&>blockquote]:border-[var(--color-bordeaux)] [&>blockquote]:pl-5 [&>blockquote]:italic",
  "[&>blockquote]:text-[18px] [&>blockquote]:leading-[1.62] md:[&>blockquote]:text-[clamp(19px,1.4vw,20px)]",
  "[&>ul]:list-disc [&>ol]:list-decimal [&>ul]:pl-7 [&>ol]:pl-7",
  "[&_li]:mt-2 [&_li]:text-[18px] [&_li]:leading-[1.62] md:[&_li]:text-[clamp(19px,1.4vw,20px)]",
  "[&_a]:text-[var(--treatment-ink)] [&_a]:underline [&_a]:decoration-[1px] [&_a]:underline-offset-[0.18em]",
  "[&>h2]:mt-14 [&>h2]:font-display [&>h2]:text-[clamp(24px,2.4vw,30px)] [&>h2]:font-normal [&>h2]:leading-[1.18]",
  "[&>h3]:mt-12 [&>h3]:font-display [&>h3]:text-[clamp(20px,2vw,24px)] [&>h3]:font-normal [&>h3]:leading-[1.22]",
].join(" ");

function ordinal(n: number): string {
  const teens = n % 100;
  if (teens >= 11 && teens <= 13) return `${n}th`;
  switch (n % 10) {
    case 1:
      return `${n}st`;
    case 2:
      return `${n}nd`;
    case 3:
      return `${n}rd`;
    default:
      return `${n}th`;
  }
}

// Same ordinal voice everywhere the letter appears, so the index date is the
// same object that opens the full letter.
export function formatLetterDate(iso: string): string {
  const d = new Date(`${iso}T12:00:00Z`);
  const month = d.toLocaleDateString("en-US", { month: "long", timeZone: "UTC" });
  return `${month} ${ordinal(d.getUTCDate())}, ${d.getUTCFullYear()}`;
}

// The letter's opening, in plain text. The index renders the real first words
// of the letter, not frontmatter written about it.
export function excerptOf(bodyHtml: string): string {
  const text = bodyHtml
    .replace(/<\/(p|h[1-6]|li|blockquote|pre)>/gi, " ")
    .replace(/<[^>]+>/g, "")
    .replace(/&#39;/g, "'")
    .replace(/&quot;/g, '"')
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">")
    .replace(/&amp;/g, "&")
    .replace(/\s+/g, " ")
    .trim();
  if (text.length <= 360) return text;
  const cut = text.slice(0, 360);
  const lastSpace = cut.lastIndexOf(" ");
  return lastSpace > 260 ? cut.slice(0, lastSpace) : cut;
}

// Only received correspondence opens with a salutation, so only it takes the
// trailing comma. A dispatch's title is a headline being announced, not a
// person being addressed.
export function formatLetterSalutation(letter: Letter): string {
  if (letter.kind !== "correspondence") return letter.title;
  return letter.title.endsWith(",") ? letter.title : `${letter.title},`;
}

export function LetterDate({
  letter,
  scale = "post",
}: {
  readonly letter: Letter;
  readonly scale?: "index" | "post";
}) {
  const metrics =
    scale === "index"
      ? { fontSize: "clamp(34px,8vw,40px)", lineHeight: "52px" }
      : { fontSize: "clamp(38px,9vw,44px)", lineHeight: "56px" };

  return (
    <p
      data-letter-slot="date"
      className={LETTER_DATE_CLASS}
      style={{
        ...metrics,
        margin: 0,
        fontFamily: lettersSignatureFont.stack,
      }}
    >
      <span
        data-letter-transition-slot="date"
        style={{ ...transitionStyle(letter, "date"), display: "inline-block" }}
      >
        {formatLetterDate(letter.publishedAt)}
      </span>
    </p>
  );
}

export function LetterSalutation({ letter }: { readonly letter: Letter }) {
  // Dispatches open straight into the body under the date — they were never
  // written to a title. Only received correspondence opens with a salutation
  // ("Dear Shovon,"). The frontmatter title still drives the <head> + OG card.
  if (letter.kind !== "correspondence") return null;
  return (
    <p
      data-letter-slot="salutation"
      className={LETTER_SALUTATION_CLASS}
      style={{
        margin: 0,
        marginTop: "28px",
        fontSize: "clamp(20px,1.6vw,22px)",
        lineHeight: 1.4,
      }}
    >
      <span
        data-letter-transition-slot="salutation"
        style={{ ...transitionStyle(letter, "salutation"), display: "inline-block" }}
      >
        {formatLetterSalutation(letter)}
      </span>
    </p>
  );
}

export function LetterExcerpt({
  letter,
  excerpt,
}: {
  readonly letter: Letter;
  readonly excerpt: string;
}) {
  if (!excerpt) return null;
  return (
    <p
      aria-hidden
      data-letter-slot="body"
      data-letter-transition-slot="body"
      className={`${LETTER_BODY_CLASS} mt-7`}
      style={{
        ...transitionStyle(letter, "body"),
        marginBottom: 0,
        maxHeight: "calc(1.62em * 4)",
        overflow: "hidden",
        WebkitMaskImage: "linear-gradient(to bottom, #000 0 82%, transparent 100%)",
        maskImage: "linear-gradient(to bottom, #000 0 82%, transparent 100%)",
      }}
    >
      {excerpt}
    </p>
  );
}

export function LetterBody({ letter }: { readonly letter: Letter }) {
  const leadHtml = letter.leadHtml || letter.bodyHtml;
  const continuationHtml = letter.continuationHtml.trim();

  return (
    <div data-letter-body data-letter-slot="body" className="mt-7">
      {leadHtml ? (
        <div
          data-letter-body-lead
          data-letter-transition-slot="body"
          className={letterProseClassName}
          style={transitionStyle(letter, "body")}
          // The body is markdown rendered to HTML at build time by the
          // company:letters-markdown Vite plugin; Tailwind child selectors keep
          // the correspondence typography local to this route.
          dangerouslySetInnerHTML={{ __html: leadHtml }}
        />
      ) : null}
      {continuationHtml ? (
        <div
          ref={syncLetterContinuationMetrics}
          data-letter-continuation
          className={`${letterProseClassName} mt-7`}
          dangerouslySetInnerHTML={{ __html: continuationHtml }}
        />
      ) : null}
    </div>
  );
}
