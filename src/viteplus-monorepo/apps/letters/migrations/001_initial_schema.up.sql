-- Letters blog schema
-- Posts stored as Tiptap (ProseMirror) JSON, synced to browser via ElectricSQL.

CREATE TABLE posts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug TEXT UNIQUE NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    subtitle TEXT NOT NULL DEFAULT '',
    cover_image_url TEXT NOT NULL DEFAULT '',
    content JSONB NOT NULL DEFAULT '{}',
    author_name TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'draft',
    published_at TIMESTAMPTZ,
    reading_time_minutes INT NOT NULL DEFAULT 0,
    total_claps INT NOT NULL DEFAULT 0,
    tags TEXT[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE claps (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    post_id UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    session_id TEXT NOT NULL,
    count INT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(post_id, session_id)
);

CREATE INDEX idx_claps_post_id ON claps(post_id);
CREATE INDEX idx_posts_status ON posts(status);
CREATE INDEX idx_posts_slug ON posts(slug);

-- Keep posts.total_claps in sync with the claps table.
-- Electric syncs the posts table; this denormalization avoids syncing claps.
CREATE OR REPLACE FUNCTION update_post_total_claps() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        UPDATE posts SET total_claps = (
            SELECT COALESCE(SUM(count), 0) FROM claps WHERE post_id = OLD.post_id
        ), updated_at = now()
        WHERE id = OLD.post_id;
        RETURN OLD;
    ELSE
        UPDATE posts SET total_claps = (
            SELECT COALESCE(SUM(count), 0) FROM claps WHERE post_id = NEW.post_id
        ), updated_at = now()
        WHERE id = NEW.post_id;
        RETURN NEW;
    END IF;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER clap_count_sync
AFTER INSERT OR UPDATE OR DELETE ON claps
FOR EACH ROW EXECUTE FUNCTION update_post_total_claps();
