import { generateHTML } from "@tiptap/html";
import StarterKit from "@tiptap/starter-kit";
import Image from "@tiptap/extension-image";
import Link from "@tiptap/extension-link";

const extensions = [StarterKit, Image, Link.configure({ openOnClick: false })];

interface TiptapRendererProps {
  content: unknown;
  className?: string;
}

export function TiptapRenderer({ content, className }: TiptapRendererProps) {
  if (!content || typeof content !== "object") {
    return <div className={className} />;
  }

  // Parse string content (Electric serializes JSONB as string)
  let doc: Record<string, unknown>;
  if (typeof content === "string") {
    try {
      doc = JSON.parse(content);
    } catch {
      return <div className={className}>{String(content)}</div>;
    }
  } else {
    doc = content as Record<string, unknown>;
  }

  // Empty document
  if (!doc.content || !Array.isArray(doc.content) || doc.content.length === 0) {
    return <div className={className} />;
  }

  const html = generateHTML(doc as any, extensions);

  return (
    <div className={className} dangerouslySetInnerHTML={{ __html: html }} />
  );
}
