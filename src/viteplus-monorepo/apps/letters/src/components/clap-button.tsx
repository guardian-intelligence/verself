import { useState, useCallback, useRef } from "react";
import { getSessionId } from "~/lib/session";
import { clapPost } from "~/server-fns/claps";

interface ClapButtonProps {
  slug: string;
  totalClaps: number;
}

export function ClapButton({ slug, totalClaps: initialTotal }: ClapButtonProps) {
  const [sessionCount, setSessionCount] = useState(0);
  const [displayTotal, setDisplayTotal] = useState(initialTotal);
  const [isAnimating, setIsAnimating] = useState(false);
  const pendingRef = useRef(0);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const flushClaps = useCallback(
    async (count: number) => {
      try {
        const result = await clapPost({
          data: { slug, sessionId: getSessionId(), count },
        });
        setSessionCount(result.sessionCount);
        setDisplayTotal(result.totalClaps);
      } catch {
        // Silently fail — clap is best-effort
      }
    },
    [slug],
  );

  const handleClap = useCallback(() => {
    if (sessionCount + pendingRef.current >= 50) return;

    pendingRef.current += 1;
    setDisplayTotal((t) => t + 1);
    setSessionCount((c) => c + 1);

    // Animate
    setIsAnimating(true);
    setTimeout(() => setIsAnimating(false), 300);

    // Debounce: batch claps into one request
    if (timerRef.current) clearTimeout(timerRef.current);
    timerRef.current = setTimeout(() => {
      const count = pendingRef.current;
      pendingRef.current = 0;
      void flushClaps(count);
    }, 400);
  }, [sessionCount, flushClaps]);

  const atMax = sessionCount >= 50;

  return (
    <div className="fixed bottom-8 left-8 z-40 flex flex-col items-center gap-1">
      <button
        onClick={handleClap}
        disabled={atMax}
        className={`
          w-14 h-14 rounded-full border-2 border-border bg-white shadow-lg
          flex items-center justify-center
          hover:shadow-xl hover:border-success transition-all
          disabled:opacity-50 disabled:cursor-not-allowed
          ${isAnimating ? "clap-animate" : ""}
        `}
        title={atMax ? "Maximum claps reached" : "Clap for this post"}
        aria-label={`Clap (${displayTotal} total)`}
      >
        <svg
          viewBox="0 0 24 24"
          fill={sessionCount > 0 ? "currentColor" : "none"}
          stroke="currentColor"
          strokeWidth={1.5}
          className={`w-6 h-6 ${sessionCount > 0 ? "text-success" : "text-foreground"}`}
        >
          <path
            strokeLinecap="round"
            strokeLinejoin="round"
            d="M6.633 10.25c.806 0 1.533-.446 2.031-1.08a9.041 9.041 0 0 1 2.861-2.4c.723-.384 1.35-.956 1.653-1.715a4.498 4.498 0 0 0 .322-1.672V2.75a.75.75 0 0 1 .75-.75 2.25 2.25 0 0 1 2.25 2.25c0 1.152-.26 2.243-.723 3.218-.266.558.107 1.282.725 1.282m0 0h3.126c1.026 0 1.945.694 2.054 1.715.045.422.068.85.068 1.285a11.95 11.95 0 0 1-2.649 7.521c-.388.482-.987.729-1.605.729H13.48c-.483 0-.964-.078-1.423-.23l-3.114-1.04a4.501 4.501 0 0 0-1.423-.23H5.904m10.598-9.75H14.25M5.904 18.5c.083.205.173.405.27.602.197.4-.078.898-.523.898h-.908c-.889 0-1.713-.518-1.972-1.368a12 12 0 0 1-.521-3.507c0-1.553.295-3.036.831-4.398C3.387 9.953 4.167 9.5 5 9.5h1.053c.472 0 .745.556.5.96a8.958 8.958 0 0 0-1.302 4.665c0 1.194.232 2.333.654 3.375Z"
          />
        </svg>
      </button>
      <span className="text-sm font-medium text-muted-foreground tabular-nums">
        {displayTotal > 0 ? displayTotal : ""}
      </span>
    </div>
  );
}
