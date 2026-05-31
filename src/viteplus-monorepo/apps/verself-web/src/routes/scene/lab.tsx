import { createFileRoute } from "@tanstack/react-router";
import { WordmarkLab } from "~/features/scene3d/WordmarkLab";

export const Route = createFileRoute("/scene/lab")({
  component: WordmarkLab,
  head: () => ({
    meta: [{ title: "Verself · 3D Wordmark Lab" }],
  }),
});
