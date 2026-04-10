import { ClientOnly, createFileRoute, useNavigate } from "@tanstack/react-router";
import { useState, useEffect, useMemo, lazy, Suspense } from "react";
import { useLiveQuery } from "@tanstack/react-db";
import { createAllPostsCollection, type ElectricPost } from "~/lib/collections";
import { formatPostDate, parsePostContent, parsePostTags } from "~/lib/post-utils";
import type { JsonValue } from "~/server-fns/validation";
import {
  updatePost,
  publishPost,
  unpublishPost,
  deletePost,
  getPostBySlug,
} from "~/server-fns/posts";

const TiptapEditor = lazy(() =>
  import("~/components/tiptap-editor").then((m) => ({ default: m.TiptapEditor })),
);

export const Route = createFileRoute("/editor/$slug")({
  component: EditPostPage,
  loader: async ({ params }) => {
    const post = await getPostBySlug({ data: { slug: params.slug } });
    return { post };
  },
});

function EditPostPage() {
  const { post: initialPost } = Route.useLoaderData();
  if (!initialPost) {
    return (
      <div className="py-12 text-center">
        <h1 className="text-xl font-bold mb-2">Post not found</h1>
        <p className="text-muted-foreground">This post may have been deleted.</p>
      </div>
    );
  }

  return (
    <ClientOnly fallback={<EditPostPageContent post={initialPost} />}>
      <LiveEditPostPage initialPost={initialPost} />
    </ClientOnly>
  );
}

function LiveEditPostPage({
  initialPost,
}: {
  initialPost: NonNullable<Awaited<ReturnType<typeof getPostBySlug>>>;
}) {
  const { slug } = Route.useParams();
  const collection = useMemo(() => createAllPostsCollection(), []);
  const { data: livePosts } = useLiveQuery((q) => q.from({ p: collection }), [collection]);
  const livePost = useMemo(
    () => (livePosts as ElectricPost[] | undefined)?.find((post) => post.slug === slug),
    [livePosts, slug],
  );
  return <EditPostPageContent post={livePost ?? initialPost} />;
}

function EditPostPageContent({
  post,
}: {
  post: NonNullable<Awaited<ReturnType<typeof getPostBySlug>>> | ElectricPost;
}) {
  const { slug } = Route.useParams();
  const navigate = useNavigate();

  const [title, setTitle] = useState(post?.title ?? "");
  const [subtitle, setSubtitle] = useState(post?.subtitle ?? "");
  const [coverImageUrl, setCoverImageUrl] = useState(post?.cover_image_url ?? "");
  const [authorName, setAuthorName] = useState(post?.author_name ?? "");
  const [tags, setTags] = useState("");
  const [content, setContent] = useState<JsonValue>();
  const [saving, setSaving] = useState(false);
  const [initialized, setInitialized] = useState(false);

  // Initialize form state from post data once
  useEffect(() => {
    if (post && !initialized) {
      setTitle(post.title);
      setSubtitle(post.subtitle);
      setCoverImageUrl(post.cover_image_url);
      setAuthorName(post.author_name);
      setTags(parsePostTags(post.tags).join(", "));
      setContent(parsePostContent(post.content));
      setInitialized(true);
    }
  }, [post, initialized]);

  async function handleSave() {
    setSaving(true);
    try {
      await updatePost({
        data: {
          slug,
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
    } catch (err) {
      console.error("Failed to save:", err);
    } finally {
      setSaving(false);
    }
  }

  async function handlePublish() {
    setSaving(true);
    try {
      await handleSave();
      await publishPost({ data: { slug } });
    } finally {
      setSaving(false);
    }
  }

  async function handleUnpublish() {
    setSaving(true);
    try {
      await unpublishPost({ data: { slug } });
    } finally {
      setSaving(false);
    }
  }

  async function handleDelete() {
    if (!window.confirm("Delete this post permanently?")) return;
    await deletePost({ data: { slug } });
    await navigate({ to: "/editor" });
  }

  const isPublished = post.status === "published";

  return (
    <div>
      <div className="flex items-center gap-3 mb-6">
        <button
          onClick={() => void handleSave()}
          disabled={saving}
          className="px-4 py-1.5 rounded-md border border-border hover:bg-muted text-sm disabled:opacity-50"
        >
          {saving ? "Saving..." : "Save"}
        </button>
        {isPublished ? (
          <button
            onClick={() => void handleUnpublish()}
            disabled={saving}
            className="px-4 py-1.5 rounded-md border border-border hover:bg-muted text-sm disabled:opacity-50"
          >
            Unpublish
          </button>
        ) : (
          <button
            onClick={() => void handlePublish()}
            disabled={saving}
            className="px-4 py-1.5 rounded-md bg-success text-white hover:bg-success/90 text-sm disabled:opacity-50"
          >
            Publish
          </button>
        )}
        <div className="ml-auto">
          <button
            onClick={() => void handleDelete()}
            className="px-3 py-1.5 rounded-md text-sm text-red-600 hover:bg-red-50"
          >
            Delete
          </button>
        </div>
      </div>

      {/* Status indicator */}
      <div className="mb-6 text-sm text-muted-foreground">
        Status:{" "}
        <span className={isPublished ? "text-success font-medium" : ""}>
          {isPublished ? "Published" : "Draft"}
        </span>
        {isPublished && post.published_at && (
          <span> · Published {formatPostDate(post.published_at)}</span>
        )}
        {post.total_claps > 0 && <span> · {post.total_claps} claps</span>}
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
      {initialized && (
        <Suspense
          fallback={
            <div className="py-12 text-center text-muted-foreground">Loading editor...</div>
          }
        >
          <TiptapEditor content={content} onChange={setContent} />
        </Suspense>
      )}
    </div>
  );
}
