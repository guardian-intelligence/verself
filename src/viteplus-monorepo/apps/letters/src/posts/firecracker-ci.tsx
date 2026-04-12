import type { Post } from "./types";

export const post: Post = {
  meta: {
    slug: "firecracker-ci",
    title: "A Firecracker CI baseline",
    subtitle: "What 22 seconds of clean-room CI buys you",
    description:
      "Notes on running real Next.js CI inside Firecracker microVMs on a single bare metal box.",
    publishedAt: "2026-03-28",
    author: "Forge Metal",
    readingMinutes: 4,
  },
  Body: () => (
    <>
      <p>
        We boot a Firecracker microVM, run a real Next.js install + lint + typecheck + build, and
        tear it down. End to end: about 22 seconds. The interesting part is not the wall clock; it
        is the absence of a runner pool, the absence of warm caches, and the absence of bespoke
        steps to make hosted CI feel acceptable.
      </p>
      <h2>The setup</h2>
      <p>
        A two-layer golden image. The base layer is a stock Ubuntu rootfs with Node and pnpm
        installed once. The product layer is a ZFS clone of that base, plus the repository under
        test pre-staged. Booting a VM is a clone, a TAP allocation, and a jailer exec. There is no
        registry pull, no container image cache, no DNS warmup.
      </p>
      <h2>What ZFS gives you</h2>
      <p>
        Cheap clones. The clone of a 2&nbsp;GB rootfs takes single-digit milliseconds and consumes
        no extra disk until something writes to it. We get the isolation properties of a fresh VM
        and the IO characteristics of a warm host. Combine that with Firecracker's startup time and
        the per-job overhead approaches whatever your kernel needs to print its first line.
      </p>
      <h2>What it does not solve</h2>
      <p>
        Cross-job caching. Each VM is clean by construction, which is the point, but it means an
        opt-in mechanism for things like the pnpm store has to live above the VM boundary. We export
        a read-only cache dataset and mount it via virtio-fs; writes get redirected to a per-attempt
        overlay that we discard at teardown. The discipline matters: a cache that can be written to
        from a job is not a cache, it is a covert channel.
      </p>
    </>
  ),
};
