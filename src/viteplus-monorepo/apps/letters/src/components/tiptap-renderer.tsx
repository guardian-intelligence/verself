import type { JSONContent } from "@tiptap/core";
import { generateHTML } from "@tiptap/html";
import StarterKit from "@tiptap/starter-kit";
import Image from "@tiptap/extension-image";
import Link from "@tiptap/extension-link";

const extensions = [StarterKit, Image, Link.configure({ openOnClick: false })];

interface TiptapRendererProps {
  content: unknown;
  className?: string;
}

function isJSONContent(value: unknown): value is JSONContent {
  return typeof value === "object" && value !== null;
}

function fallbackText(value: unknown): string {
  if (typeof value === "number" || typeof value === "boolean" || typeof value === "bigint") {
    return value.toString();
  }
  return "";
}

export function TiptapRenderer({ content, className }: TiptapRendererProps) {
  if (!content) {
    return <div className={className} />;
  }

  let doc: JSONContent;
  if (typeof content === "string") {
    try {
      const parsed = JSON.parse(content);
      if (!isJSONContent(parsed)) {
        return <div className={className}>{content}</div>;
      }
      doc = parsed;
    } catch {
      return <div className={className}>{content}</div>;
    }
  } else {
    if (!isJSONContent(content)) {
      return <div className={className}>{fallbackText(content)}</div>;
    }
    doc = content;
  }

  // Empty document
  if (!doc.content || !Array.isArray(doc.content) || doc.content.length === 0) {
    return <div className={className} />;
  }

  const html = generateHTML(doc, extensions);

  return <div className={className} dangerouslySetInnerHTML={{ __html: html }} />;
}
