import { useEditor, EditorContent, BubbleMenu } from "@tiptap/react";
import StarterKit from "@tiptap/starter-kit";
import Image from "@tiptap/extension-image";
import LinkExtension from "@tiptap/extension-link";
import Placeholder from "@tiptap/extension-placeholder";
import { useCallback } from "react";

interface TiptapEditorProps {
  content: unknown;
  onChange: (content: unknown) => void;
}

export function TiptapEditor({ content, onChange }: TiptapEditorProps) {
  const editor = useEditor({
    extensions: [
      StarterKit.configure({
        heading: { levels: [2, 3] },
      }),
      Image,
      LinkExtension.configure({
        openOnClick: false,
      }),
      Placeholder.configure({
        placeholder: "Tell your story...",
      }),
    ],
    content: parseContent(content),
    onUpdate: ({ editor: e }) => {
      onChange(e.getJSON());
    },
    editorProps: {
      attributes: {
        class: "prose-letters focus:outline-none",
      },
    },
  });

  const addImage = useCallback(() => {
    if (!editor) return;
    const url = window.prompt("Image URL:");
    if (url) {
      editor.chain().focus().setImage({ src: url }).run();
    }
  }, [editor]);

  const addLink = useCallback(() => {
    if (!editor) return;
    const url = window.prompt("Link URL:");
    if (url) {
      editor.chain().focus().setLink({ href: url }).run();
    }
  }, [editor]);

  if (!editor) return null;

  return (
    <div className="tiptap-editor">
      {/* Medium-style floating toolbar on text selection */}
      <BubbleMenu
        editor={editor}
        tippyOptions={{ duration: 150 }}
        className="flex items-center gap-0.5 bg-foreground rounded-lg shadow-lg px-1 py-0.5"
      >
        <ToolbarButton
          active={editor.isActive("bold")}
          onClick={() => editor.chain().focus().toggleBold().run()}
          label="Bold"
        >
          B
        </ToolbarButton>
        <ToolbarButton
          active={editor.isActive("italic")}
          onClick={() => editor.chain().focus().toggleItalic().run()}
          label="Italic"
        >
          <em>I</em>
        </ToolbarButton>
        <ToolbarButton
          active={editor.isActive("strike")}
          onClick={() => editor.chain().focus().toggleStrike().run()}
          label="Strikethrough"
        >
          <s>S</s>
        </ToolbarButton>
        <ToolbarButton
          active={editor.isActive("code")}
          onClick={() => editor.chain().focus().toggleCode().run()}
          label="Code"
        >
          {"<>"}
        </ToolbarButton>
        <div className="w-px h-5 bg-background/30 mx-0.5" />
        <ToolbarButton
          active={editor.isActive("heading", { level: 2 })}
          onClick={() => editor.chain().focus().toggleHeading({ level: 2 }).run()}
          label="Heading 2"
        >
          H2
        </ToolbarButton>
        <ToolbarButton
          active={editor.isActive("heading", { level: 3 })}
          onClick={() => editor.chain().focus().toggleHeading({ level: 3 }).run()}
          label="Heading 3"
        >
          H3
        </ToolbarButton>
        <div className="w-px h-5 bg-background/30 mx-0.5" />
        <ToolbarButton
          active={editor.isActive("blockquote")}
          onClick={() => editor.chain().focus().toggleBlockquote().run()}
          label="Quote"
        >
          &ldquo;
        </ToolbarButton>
        <ToolbarButton active={editor.isActive("link")} onClick={addLink} label="Link">
          🔗
        </ToolbarButton>
      </BubbleMenu>

      {/* Block-level controls */}
      <div className="flex gap-2 mb-4 border-b border-border pb-3">
        <button
          onClick={addImage}
          className="text-sm px-3 py-1 rounded border border-border hover:bg-muted text-muted-foreground"
        >
          + Image
        </button>
        <button
          onClick={() => editor.chain().focus().toggleCodeBlock().run()}
          className="text-sm px-3 py-1 rounded border border-border hover:bg-muted text-muted-foreground"
        >
          + Code Block
        </button>
        <button
          onClick={() => editor.chain().focus().setHorizontalRule().run()}
          className="text-sm px-3 py-1 rounded border border-border hover:bg-muted text-muted-foreground"
        >
          + Divider
        </button>
      </div>

      <EditorContent editor={editor} />
    </div>
  );
}

function ToolbarButton({
  active,
  onClick,
  label,
  children,
}: {
  active: boolean;
  onClick: () => void;
  label: string;
  children: React.ReactNode;
}) {
  return (
    <button
      onClick={onClick}
      aria-label={label}
      className={`
        px-2 py-1 text-sm rounded font-medium transition-colors
        ${active ? "text-white bg-white/20" : "text-white/70 hover:text-white"}
      `}
    >
      {children}
    </button>
  );
}

function parseContent(content: unknown): Record<string, unknown> | undefined {
  if (!content) return undefined;
  if (typeof content === "string") {
    try {
      return JSON.parse(content);
    } catch {
      return undefined;
    }
  }
  return content as Record<string, unknown>;
}
