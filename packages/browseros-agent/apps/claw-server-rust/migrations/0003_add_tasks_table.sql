CREATE TABLE IF NOT EXISTS tasks (
    session_id TEXT PRIMARY KEY NOT NULL,
    agent_id TEXT NOT NULL,
    slug TEXT NOT NULL,
    agent_label TEXT NOT NULL,
    title TEXT NOT NULL,
    site TEXT,
    started_at INTEGER NOT NULL,
    ended_at INTEGER,
    duration_ms INTEGER NOT NULL,
    dispatch_count INTEGER NOT NULL,
    tool_sequence_json TEXT NOT NULL,
    status TEXT NOT NULL,
    error_count INTEGER NOT NULL,
    last_screenshot_dispatch_id INTEGER,
    cursor_id INTEGER NOT NULL,
    has_screenshots INTEGER NOT NULL DEFAULT 0,
    updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS tasks_cursor_idx ON tasks (cursor_id DESC);
CREATE INDEX IF NOT EXISTS tasks_agent_cursor_idx ON tasks (agent_id, cursor_id DESC);
CREATE INDEX IF NOT EXISTS tasks_status_cursor_idx ON tasks (status, cursor_id DESC);
CREATE INDEX IF NOT EXISTS tasks_site_cursor_idx ON tasks (site, cursor_id DESC);
CREATE INDEX IF NOT EXISTS tasks_started_idx ON tasks (started_at);
