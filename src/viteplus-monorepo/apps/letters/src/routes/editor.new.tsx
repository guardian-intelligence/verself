import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useState, lazy, Suspense } from "react";
import { createPost, publishPost } from "~/server-fns/posts";

const TiptapEditor = lazy(() =>
  import("~/components/tiptap-editor").then((m) => ({ default: m.TiptapEditor })),
);

export const Route = createFileRoute("/editor/new")({
  component: NewPostPage,
});

function NewPostPage() {
  const navigate = useNavigate();
  const [title, setTitle] = useState("");
  const [subtitle, setSubtitle] = useState("");
  const [coverImageUrl, setCoverImageUrl] = useState("");
  const [authorName, setAuthorName] = useState("");
  const [tags, setTags] = useState("");
  const [content, setContent] = useState<unknown>(null);
  const [saving, setSaving] = useState(false);

  async function handleSave(publish: boolean) {
    if (!title.trim()) return;
    setSaving(true);
    try {
      const result = await createPost({
        data: {
          title,
          subtitle,
          cover_image_url: coverImageUrl,
          author_name: authorName,
          content,
          tags: tags
            .split(",")
            .map((t) => t.trim())
            .filter(Boolean),
        },
      });
      if (publish) {
        await publishPost({ data: { slug: result.slug } });
      }
      void navigate({ to: "/editor/$slug", params: { slug: result.slug } });
    } catch (err) {
      console.error("Failed to save:", err);
    } finally {
      setSaving(false);
    }
  }

  return (
    <div>
      <div className="flex items-center gap-3 mb-6">
        <button
          onClick={() => void handleSave(false)}
          disabled={saving || !title.trim()}
          className="px-4 py-1.5 rounded-md border border-border hover:bg-muted text-sm disabled:opacity-50"
        >
          Save Draft
        </button>
        <button
          onClick={() => void handleSave(true)}
          disabled={saving || !title.trim()}
          className="px-4 py-1.5 rounded-md bg-success text-white hover:bg-success/90 text-sm disabled:opacity-50"
        >
          Publish
        </button>
      </div>

      {/* Metadata */}
      <div className="grid grid-cols-2 gap-4 mb-8">
        <div className="col-span-2">
          <input
            type="text"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="Post title"
            className="w-full text-3xl font-bold border-none outline-none bg-transparent placeholder:text-muted-foreground/40"
            style={{ fontFamily: "'Playfair Display', serif" }}
          />
        </div>
        <div className="col-span-2">
          <input
            type="text"
            value={subtitle}
            onChange={(e) => setSubtitle(e.target.value)}
            placeholder="Subtitle (optional)"
            className="w-full text-lg border-none outline-none bg-transparent text-muted-foreground placeholder:text-muted-foreground/40"
          />
        </div>
        <input
          type="text"
          value={coverImageUrl}
          onChange={(e) => setCoverImageUrl(e.target.value)}
          placeholder="Cover image URL"
          className="px-3 py-2 rounded-md border border-border text-sm bg-transparent"
        />
        <input
          type="text"
          value={authorName}
          onChange={(e) => setAuthorName(e.target.value)}
          placeholder="Author name"
          className="px-3 py-2 rounded-md border border-border text-sm bg-transparent"
        />
        <input
          type="text"
          value={tags}
          onChange={(e) => setTags(e.target.value)}
          placeholder="Tags (comma-separated)"
          className="col-span-2 px-3 py-2 rounded-md border border-border text-sm bg-transparent"
        />
      </div>

      {/* Editor */}
      <Suspense
        fallback={<div className="py-12 text-center text-muted-foreground">Loading editor...</div>}
      >
        <TiptapEditor content={content} onChange={setContent} />
      </Suspense>
    </div>
  );
}
