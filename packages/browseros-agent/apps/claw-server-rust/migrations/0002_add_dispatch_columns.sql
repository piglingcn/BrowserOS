ALTER TABLE tool_dispatches ADD COLUMN dispatch_id TEXT;
ALTER TABLE tool_dispatches ADD COLUMN has_screenshot INTEGER NOT NULL DEFAULT 0;
