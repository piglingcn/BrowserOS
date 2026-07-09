use crate::{
    domain::DispatchId,
    error::{AppError, AppResult, IoPath},
    services::now_epoch_ms,
};
use rusqlite::{Connection, OptionalExtension, params, types::Value};
use rusqlite_migration::{M, Migrations};
use serde::{Deserialize, Serialize};
use serde_json::json;
use std::{
    path::{Path, PathBuf},
    sync::Arc,
};
use tokio::sync::Mutex;
use url::Url;

const AUDIT_0000: &str = include_str!("../../migrations/0000_add_tool_dispatches.sql");
const AUDIT_0001: &str = include_str!("../../migrations/0001_add_agent_session_events.sql");
const AUDIT_0002: &str = include_str!("../../migrations/0002_add_dispatch_columns.sql");
const AUDIT_0003: &str = include_str!("../../migrations/0003_add_tasks_table.sql");
const CURRENT_AUDIT_SCHEMA_VERSION: usize = 4;
const ARGS_JSON_MAX: usize = 4096;

struct DrizzleCompatMigration {
    tag: &'static str,
    created_at: i64,
}

const DRIZZLE_COMPAT_MIGRATIONS: [DrizzleCompatMigration; 2] = [
    DrizzleCompatMigration {
        tag: "0000_add_tool_dispatches",
        created_at: 1782320133071,
    },
    DrizzleCompatMigration {
        tag: "0001_add_agent_session_events",
        created_at: 1782387594647,
    },
];

#[derive(Clone)]
pub struct AuditService {
    conn: Arc<Mutex<Connection>>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ToolDispatchRow {
    pub id: i64,
    pub created_at: i64,
    pub agent_id: String,
    pub slug: String,
    pub agent_label: String,
    pub session_id: String,
    pub tool_name: String,
    pub page_id: Option<i64>,
    pub target_id: Option<String>,
    pub url: Option<String>,
    pub title: Option<String>,
    pub args_json: Option<String>,
    pub result_meta: Option<String>,
    pub duration_ms: Option<i64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub dispatch_id: Option<String>,
    pub has_screenshot: bool,
}

#[derive(Debug, Clone)]
pub struct RecordToolDispatchInput {
    pub agent_id: String,
    pub slug: String,
    pub agent_label: String,
    pub session_id: String,
    pub tool_name: String,
    pub page_id: Option<i64>,
    pub target_id: Option<String>,
    pub url: Option<String>,
    pub title: Option<String>,
    pub raw_args: serde_json::Value,
    pub duration_ms: i64,
    pub dispatch_id: DispatchId,
    pub result: DispatchResultSummary,
}

#[derive(Debug, Clone)]
pub struct DispatchResultSummary {
    pub is_error: bool,
    pub structured_content: serde_json::Value,
    pub content: serde_json::Value,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ListDispatchesResult {
    pub rows: Vec<ToolDispatchRow>,
    pub next_cursor: Option<i64>,
}

#[derive(Debug, Clone, Default)]
pub struct ListDispatchesQuery {
    pub agent_id: Option<String>,
    pub session_id: Option<String>,
    pub cursor: Option<i64>,
    pub limit: Option<i64>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum TaskStatus {
    Live,
    Done,
    Failed,
}

impl TaskStatus {
    fn as_str(self) -> &'static str {
        match self {
            Self::Live => "live",
            Self::Done => "done",
            Self::Failed => "failed",
        }
    }

    fn from_db(value: String) -> Self {
        match value.as_str() {
            "done" => Self::Done,
            "failed" => Self::Failed,
            _ => Self::Live,
        }
    }
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct TaskSummary {
    pub session_id: String,
    pub agent_id: String,
    pub slug: String,
    pub agent_label: String,
    pub title: String,
    pub site: Option<String>,
    pub started_at: i64,
    pub ended_at: Option<i64>,
    pub duration_ms: i64,
    pub dispatch_count: i64,
    pub tool_sequence: Vec<String>,
    pub status: TaskStatus,
    pub error_count: i64,
    pub last_screenshot_dispatch_id: Option<i64>,
    pub cursor_id: i64,
    pub has_screenshots: bool,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct TaskDetail {
    #[serde(flatten)]
    pub summary: TaskSummary,
    pub dispatches: Vec<ToolDispatchRow>,
    pub screenshot_dispatch_ids: Vec<i64>,
    pub start_event: Option<SessionStartEvent>,
    pub end_event: Option<SessionEndEvent>,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct SessionStartEvent {
    pub created_at: i64,
    pub client_name: String,
    pub client_version: String,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct SessionEndEvent {
    pub created_at: i64,
    pub kind: String,
    pub reason: Option<String>,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ListTasksResult {
    pub tasks: Vec<TaskSummary>,
    pub next_cursor: Option<i64>,
}

#[derive(Debug, Clone, Default)]
pub struct ListTasksQuery {
    pub agent_id: Option<String>,
    pub status: Option<TaskStatus>,
    pub site: Option<String>,
    pub search: Option<String>,
    pub since: Option<i64>,
    pub cursor: Option<i64>,
    pub limit: Option<i64>,
}

impl AuditService {
    pub async fn open(path: impl AsRef<Path>) -> AppResult<Self> {
        let path = path.as_ref().to_path_buf();
        if let Some(parent) = path.parent() {
            tokio::fs::create_dir_all(parent).await.with_path(parent)?;
        }
        let conn = tokio::task::spawn_blocking(move || open_connection(path)).await??;
        Ok(Self {
            conn: Arc::new(Mutex::new(conn)),
        })
    }

    pub async fn record_tool_dispatch(&self, input: RecordToolDispatchInput) -> AppResult<i64> {
        let mut conn = self.conn.lock().await;
        let tx = conn.transaction()?;
        let args_json = truncate(&safe_stringify(&input.raw_args));
        let result_meta = summarize_result(&input.result);
        let dispatch_id = input.dispatch_id.into_inner();
        tx.execute(
            "INSERT INTO tool_dispatches
                (agent_id, slug, agent_label, session_id, tool_name, page_id, target_id, url, title, args_json, result_meta, duration_ms, dispatch_id, has_screenshot)
             VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11, ?12, ?13, 0)",
            params![
                input.agent_id,
                input.slug,
                input.agent_label,
                input.session_id,
                input.tool_name,
                input.page_id,
                input.target_id,
                input.url,
                input.title,
                args_json,
                result_meta,
                input.duration_ms,
                dispatch_id,
            ],
        )?;
        let id = tx.last_insert_rowid();
        recompute_task(&tx, input.session_id.as_str())?;
        tx.commit()?;
        Ok(id)
    }

    pub async fn mark_screenshot(&self, dispatch_id: i64) -> AppResult<()> {
        let mut conn = self.conn.lock().await;
        let tx = conn.transaction()?;
        let session_id: Option<String> = tx
            .query_row(
                "SELECT session_id FROM tool_dispatches WHERE id = ?1",
                params![dispatch_id],
                |row| row.get(0),
            )
            .optional()?;
        tx.execute(
            "UPDATE tool_dispatches SET has_screenshot = 1 WHERE id = ?1",
            params![dispatch_id],
        )?;
        if let Some(session_id) = session_id {
            recompute_task(&tx, session_id.as_str())?;
        }
        tx.commit()?;
        Ok(())
    }

    pub async fn record_session_start(
        &self,
        session_id: &str,
        agent_id: &str,
        slug: &str,
        agent_label: &str,
        client_name: &str,
        client_version: &str,
    ) -> AppResult<()> {
        let mut conn = self.conn.lock().await;
        let tx = conn.transaction()?;
        tx.execute(
            "INSERT INTO agent_session_starts
                (session_id, agent_id, slug, agent_label, client_name, client_version)
             VALUES (?1, ?2, ?3, ?4, ?5, ?6)",
            params![
                session_id,
                agent_id,
                slug,
                agent_label,
                client_name,
                client_version
            ],
        )?;
        recompute_task(&tx, session_id)?;
        tx.commit()?;
        Ok(())
    }

    pub async fn record_session_end(
        &self,
        session_id: &str,
        kind: &str,
        reason: Option<&str>,
    ) -> AppResult<()> {
        let mut conn = self.conn.lock().await;
        let tx = conn.transaction()?;
        tx.execute(
            "INSERT INTO agent_session_ends (session_id, kind, reason) VALUES (?1, ?2, ?3)",
            params![session_id, kind, reason],
        )?;
        recompute_task(&tx, session_id)?;
        tx.commit()?;
        Ok(())
    }

    pub async fn list_dispatches(
        &self,
        query: ListDispatchesQuery,
    ) -> AppResult<ListDispatchesResult> {
        let limit = query.limit.unwrap_or(100).clamp(1, 500);
        let mut values = Vec::new();
        let mut sql = String::from(
            "SELECT id, created_at, agent_id, slug, agent_label, session_id, tool_name, page_id, target_id, url, title, args_json, result_meta, duration_ms, dispatch_id, has_screenshot FROM tool_dispatches WHERE 1 = 1",
        );
        if let Some(agent_id) = query.agent_id {
            sql.push_str(" AND agent_id = ?");
            values.push(Value::from(agent_id));
        }
        if let Some(session_id) = query.session_id {
            sql.push_str(" AND session_id = ?");
            values.push(Value::from(session_id));
        }
        if let Some(cursor) = query.cursor {
            sql.push_str(" AND id < ?");
            values.push(Value::from(cursor));
        }
        sql.push_str(" ORDER BY id DESC LIMIT ?");
        values.push(Value::from(limit + 1));
        let conn = self.conn.lock().await;
        let mut stmt = conn.prepare(&sql)?;
        let mut rows = stmt
            .query_map(rusqlite::params_from_iter(values.iter()), map_dispatch_row)?
            .collect::<Result<Vec<_>, _>>()?;
        let next_cursor = if rows.len() > usize::try_from(limit).unwrap_or(500) {
            rows.truncate(usize::try_from(limit).unwrap_or(500));
            rows.last().map(|row| row.id)
        } else {
            None
        };
        Ok(ListDispatchesResult { rows, next_cursor })
    }

    pub async fn list_tasks(&self, query: ListTasksQuery) -> AppResult<ListTasksResult> {
        let limit = query.limit.unwrap_or(25).clamp(1, 100);
        let mut values = Vec::new();
        let mut sql = String::from(
            "SELECT session_id, agent_id, slug, agent_label, title, site, started_at, ended_at, duration_ms, dispatch_count, tool_sequence_json, status, error_count, last_screenshot_dispatch_id, cursor_id, has_screenshots FROM tasks WHERE 1 = 1",
        );
        if let Some(agent_id) = query.agent_id {
            sql.push_str(" AND agent_id = ?");
            values.push(Value::from(agent_id));
        }
        if let Some(status) = query.status {
            sql.push_str(" AND status = ?");
            values.push(Value::from(status.as_str().to_string()));
        }
        if let Some(site) = query.site {
            sql.push_str(" AND site = ?");
            values.push(Value::from(site));
        }
        if let Some(since) = query.since {
            sql.push_str(" AND started_at >= ?");
            values.push(Value::from(since));
        }
        if let Some(search) = query.search {
            sql.push_str(" AND (lower(title) LIKE ? OR lower(agent_label) LIKE ? OR lower(coalesce(site, '')) LIKE ?)");
            let pattern = format!("%{}%", search.to_ascii_lowercase());
            values.push(Value::from(pattern.clone()));
            values.push(Value::from(pattern.clone()));
            values.push(Value::from(pattern));
        }
        if let Some(cursor) = query.cursor {
            sql.push_str(" AND cursor_id < ?");
            values.push(Value::from(cursor));
        }
        sql.push_str(" ORDER BY cursor_id DESC, started_at DESC LIMIT ?");
        values.push(Value::from(limit + 1));
        let conn = self.conn.lock().await;
        let mut stmt = conn.prepare(&sql)?;
        let mut tasks = stmt
            .query_map(rusqlite::params_from_iter(values.iter()), map_task_summary)?
            .collect::<Result<Vec<_>, _>>()?;
        let next_cursor = if tasks.len() > usize::try_from(limit).unwrap_or(100) {
            tasks.truncate(usize::try_from(limit).unwrap_or(100));
            tasks.last().map(|task| task.cursor_id)
        } else {
            None
        };
        Ok(ListTasksResult { tasks, next_cursor })
    }

    pub async fn get_task(&self, session_id: &str) -> AppResult<Option<TaskDetail>> {
        let conn = self.conn.lock().await;
        let summary = conn
            .query_row(
                "SELECT session_id, agent_id, slug, agent_label, title, site, started_at, ended_at, duration_ms, dispatch_count, tool_sequence_json, status, error_count, last_screenshot_dispatch_id, cursor_id, has_screenshots FROM tasks WHERE session_id = ?1",
                params![session_id],
                map_task_summary,
            )
            .optional()?;
        let Some(summary) = summary else {
            return Ok(None);
        };
        let dispatches = query_dispatches_for_session(&conn, session_id)?;
        let screenshot_dispatch_ids = dispatches
            .iter()
            .filter(|row| row.has_screenshot && !result_is_error(row.result_meta.as_deref()))
            .map(|row| row.id)
            .collect();
        let start_event = conn
            .query_row(
                "SELECT created_at, client_name, client_version FROM agent_session_starts WHERE session_id = ?1 ORDER BY id LIMIT 1",
                params![session_id],
                |row| {
                    Ok(SessionStartEvent {
                        created_at: row.get(0)?,
                        client_name: row.get(1)?,
                        client_version: row.get(2)?,
                    })
                },
            )
            .optional()?;
        let end_event = conn
            .query_row(
                "SELECT created_at, kind, reason FROM agent_session_ends WHERE session_id = ?1 ORDER BY id LIMIT 1",
                params![session_id],
                |row| {
                    Ok(SessionEndEvent {
                        created_at: row.get(0)?,
                        kind: row.get(1)?,
                        reason: row.get(2)?,
                    })
                },
            )
            .optional()?;
        Ok(Some(TaskDetail {
            summary,
            dispatches,
            screenshot_dispatch_ids,
            start_event,
            end_event,
        }))
    }
}

/// Opens the audit SQLite DB with runtime pragmas and the latest schema.
fn open_connection(path: PathBuf) -> AppResult<Connection> {
    let mut conn = Connection::open(path)?;
    conn.pragma_update(None, "journal_mode", "WAL")?;
    conn.pragma_update(None, "synchronous", "NORMAL")?;
    conn.pragma_update(None, "foreign_keys", "ON")?;
    run_migrations(&mut conn)?;
    Ok(conn)
}

/// Baselines legacy audit DBs and applies all Rust-owned schema migrations.
fn run_migrations(conn: &mut Connection) -> AppResult<()> {
    let seed_drizzle_compat = baseline_user_version(conn)? == 0;
    audit_migrations()
        .to_latest(conn)
        .map_err(|err| AppError::Internal(format!("audit migration failed: {err}")))?;
    if seed_drizzle_compat {
        seed_drizzle_migrations(conn)?;
    }
    Ok(())
}

/// Returns the ordered Rust-owned migrations tracked by SQLite user_version.
fn audit_migrations() -> Migrations<'static> {
    Migrations::new(vec![
        M::up(AUDIT_0000),
        M::up(AUDIT_0001),
        M::up(AUDIT_0002),
        M::up(AUDIT_0003),
    ])
}

/// Marks old Drizzle/Rust-created schemas before native migrations continue.
fn baseline_user_version(conn: &Connection) -> AppResult<usize> {
    let version = user_version(conn)?;
    if version != 0 {
        return Ok(version);
    }

    let baseline = detected_schema_version(conn)?;
    if baseline > 0 {
        conn.pragma_update(None, "user_version", baseline)?;
    }
    Ok(baseline)
}

/// Infers how far a pre-user_version audit DB had already migrated.
fn detected_schema_version(conn: &Connection) -> AppResult<usize> {
    if !table_exists(conn, "tool_dispatches")? {
        return Ok(0);
    }
    if !table_exists(conn, "agent_session_starts")? || !table_exists(conn, "agent_session_ends")? {
        return Ok(1);
    }
    if !column_exists(conn, "tool_dispatches", "dispatch_id")? {
        return Ok(2);
    }
    if !table_exists(conn, "tasks")? {
        return Ok(3);
    }
    Ok(CURRENT_AUDIT_SCHEMA_VERSION)
}

fn user_version(conn: &Connection) -> AppResult<usize> {
    conn.query_row("PRAGMA user_version", [], |row| row.get(0))
        .map_err(AppError::from)
}

fn table_exists(conn: &Connection, table: &str) -> AppResult<bool> {
    conn.query_row(
        "SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?1",
        params![table],
        |_| Ok(()),
    )
    .optional()
    .map(|value| value.is_some())
    .map_err(AppError::from)
}

fn column_exists(conn: &Connection, table: &str, column: &str) -> AppResult<bool> {
    let mut stmt = conn.prepare(&format!("PRAGMA table_info({table})"))?;
    let columns = stmt
        .query_map([], |row| row.get::<_, String>(1))?
        .collect::<Result<Vec<_>, _>>()?;
    Ok(columns.iter().any(|name| name == column))
}

/// Preserves TS startup compatibility only for fresh DBs created by Rust.
fn seed_drizzle_migrations(conn: &Connection) -> AppResult<()> {
    // TS-compat: delete with apps/claw-server.
    conn.execute_batch(
        r#"CREATE TABLE IF NOT EXISTS "__drizzle_migrations" (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            hash text NOT NULL,
            created_at numeric
        );"#,
    )?;
    for migration in DRIZZLE_COMPAT_MIGRATIONS {
        conn.execute(
            "INSERT INTO __drizzle_migrations (hash, created_at)
             SELECT ?1, ?2 WHERE NOT EXISTS (SELECT 1 FROM __drizzle_migrations WHERE created_at = ?2)",
            params![migration.tag, migration.created_at],
        )?;
    }
    Ok(())
}

fn recompute_task(conn: &Connection, session_id: &str) -> AppResult<()> {
    let dispatches = query_dispatches_for_session(conn, session_id)?;
    let start = query_start(conn, session_id)?;
    let end = query_end(conn, session_id)?;
    if dispatches.is_empty() && start.is_none() {
        return Ok(());
    }
    let first_dispatch = dispatches.first();
    let last_dispatch = dispatches.last();
    let started_at = start
        .as_ref()
        .map(|event| event.created_at)
        .or_else(|| first_dispatch.map(|row| row.created_at))
        .unwrap_or_else(now_epoch_ms);
    let ended_at = end.as_ref().map(|event| event.created_at);
    let agent_id = first_dispatch
        .map(|row| row.agent_id.clone())
        .or_else(|| start.as_ref().map(|event| event.agent_id.clone()))
        .unwrap_or_default();
    let slug = first_dispatch
        .map(|row| row.slug.clone())
        .or_else(|| start.as_ref().map(|event| event.slug.clone()))
        .unwrap_or_default();
    let agent_label = first_dispatch
        .map(|row| row.agent_label.clone())
        .or_else(|| start.as_ref().map(|event| event.agent_label.clone()))
        .unwrap_or_else(|| "agent".to_string());
    let site = first_site_of(&dispatches);
    let title = site
        .as_ref()
        .map(|site| format!("Browsed {site}"))
        .unwrap_or_else(|| format!("Session on {agent_label}"));
    let cursor_id = last_dispatch.map(|row| row.id).unwrap_or(0);
    let last_at = last_dispatch
        .map(|row| row.created_at)
        .unwrap_or(started_at);
    let duration_ms = ended_at.unwrap_or(last_at).saturating_sub(started_at);
    let error_count = dispatches
        .iter()
        .filter(|row| result_is_error(row.result_meta.as_deref()))
        .count() as i64;
    let status = derive_status(error_count, end.as_ref());
    let tool_sequence: Vec<String> = dispatches.iter().map(|row| row.tool_name.clone()).collect();
    let screenshot_ids: Vec<i64> = dispatches
        .iter()
        .filter(|row| row.has_screenshot && !result_is_error(row.result_meta.as_deref()))
        .map(|row| row.id)
        .collect();
    let last_screenshot_dispatch_id = screenshot_ids.last().copied();
    conn.execute(
        "INSERT INTO tasks
            (session_id, agent_id, slug, agent_label, title, site, started_at, ended_at, duration_ms, dispatch_count, tool_sequence_json, status, error_count, last_screenshot_dispatch_id, cursor_id, has_screenshots, updated_at)
         VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11, ?12, ?13, ?14, ?15, ?16, ?17)
         ON CONFLICT(session_id) DO UPDATE SET
            agent_id = excluded.agent_id,
            slug = excluded.slug,
            agent_label = excluded.agent_label,
            title = excluded.title,
            site = excluded.site,
            started_at = excluded.started_at,
            ended_at = excluded.ended_at,
            duration_ms = excluded.duration_ms,
            dispatch_count = excluded.dispatch_count,
            tool_sequence_json = excluded.tool_sequence_json,
            status = excluded.status,
            error_count = excluded.error_count,
            last_screenshot_dispatch_id = excluded.last_screenshot_dispatch_id,
            cursor_id = excluded.cursor_id,
            has_screenshots = excluded.has_screenshots,
            updated_at = excluded.updated_at",
        params![
            session_id,
            agent_id,
            slug,
            agent_label,
            title,
            site,
            started_at,
            ended_at,
            duration_ms,
            i64::try_from(dispatches.len()).unwrap_or(i64::MAX),
            serde_json::to_string(&tool_sequence)?,
            status.as_str(),
            error_count,
            last_screenshot_dispatch_id,
            cursor_id,
            if screenshot_ids.is_empty() { 0 } else { 1 },
            now_epoch_ms(),
        ],
    )?;
    Ok(())
}

fn query_dispatches_for_session(
    conn: &Connection,
    session_id: &str,
) -> AppResult<Vec<ToolDispatchRow>> {
    let mut stmt = conn.prepare("SELECT id, created_at, agent_id, slug, agent_label, session_id, tool_name, page_id, target_id, url, title, args_json, result_meta, duration_ms, dispatch_id, has_screenshot FROM tool_dispatches WHERE session_id = ?1 ORDER BY id")?;
    let rows = stmt
        .query_map(params![session_id], map_dispatch_row)?
        .collect::<Result<Vec<_>, _>>()?;
    Ok(rows)
}

#[derive(Debug)]
struct StartRow {
    created_at: i64,
    agent_id: String,
    slug: String,
    agent_label: String,
}

fn query_start(conn: &Connection, session_id: &str) -> AppResult<Option<StartRow>> {
    conn.query_row(
        "SELECT created_at, agent_id, slug, agent_label FROM agent_session_starts WHERE session_id = ?1 ORDER BY id LIMIT 1",
        params![session_id],
        |row| {
            Ok(StartRow {
                created_at: row.get(0)?,
                agent_id: row.get(1)?,
                slug: row.get(2)?,
                agent_label: row.get(3)?,
            })
        },
    )
    .optional()
    .map_err(AppError::from)
}

fn query_end(conn: &Connection, session_id: &str) -> AppResult<Option<SessionEndEvent>> {
    conn.query_row(
        "SELECT created_at, kind, reason FROM agent_session_ends WHERE session_id = ?1 ORDER BY id LIMIT 1",
        params![session_id],
        |row| {
            Ok(SessionEndEvent {
                created_at: row.get(0)?,
                kind: row.get(1)?,
                reason: row.get(2)?,
            })
        },
    )
    .optional()
    .map_err(AppError::from)
}

fn map_dispatch_row(row: &rusqlite::Row<'_>) -> rusqlite::Result<ToolDispatchRow> {
    Ok(ToolDispatchRow {
        id: row.get(0)?,
        created_at: row.get(1)?,
        agent_id: row.get(2)?,
        slug: row.get(3)?,
        agent_label: row.get(4)?,
        session_id: row.get(5)?,
        tool_name: row.get(6)?,
        page_id: row.get(7)?,
        target_id: row.get(8)?,
        url: row.get(9)?,
        title: row.get(10)?,
        args_json: row.get(11)?,
        result_meta: row.get(12)?,
        duration_ms: row.get(13)?,
        dispatch_id: row.get(14)?,
        has_screenshot: row.get::<_, i64>(15)? != 0,
    })
}

fn map_task_summary(row: &rusqlite::Row<'_>) -> rusqlite::Result<TaskSummary> {
    let tool_sequence_json: String = row.get(10)?;
    let tool_sequence =
        serde_json::from_str::<Vec<String>>(&tool_sequence_json).unwrap_or_default();
    Ok(TaskSummary {
        session_id: row.get(0)?,
        agent_id: row.get(1)?,
        slug: row.get(2)?,
        agent_label: row.get(3)?,
        title: row.get(4)?,
        site: row.get(5)?,
        started_at: row.get(6)?,
        ended_at: row.get(7)?,
        duration_ms: row.get(8)?,
        dispatch_count: row.get(9)?,
        tool_sequence,
        status: TaskStatus::from_db(row.get(11)?),
        error_count: row.get(12)?,
        last_screenshot_dispatch_id: row.get(13)?,
        cursor_id: row.get(14)?,
        has_screenshots: row.get::<_, i64>(15)? != 0,
    })
}

fn derive_status(error_count: i64, end: Option<&SessionEndEvent>) -> TaskStatus {
    if end.map(|event| event.kind.as_str()) == Some("errored") || error_count > 0 {
        TaskStatus::Failed
    } else if end.map(|event| event.kind.as_str()) == Some("closed") {
        TaskStatus::Done
    } else {
        TaskStatus::Live
    }
}

fn first_site_of(dispatches: &[ToolDispatchRow]) -> Option<String> {
    for row in dispatches {
        if let Some(url) = row.url.as_deref().and_then(hostname_of) {
            return Some(url);
        }
    }
    for row in dispatches {
        if let Some(url) = row
            .args_json
            .as_deref()
            .and_then(url_from_args)
            .and_then(|url| hostname_of(&url))
        {
            return Some(url);
        }
    }
    None
}

fn hostname_of(raw: &str) -> Option<String> {
    Url::parse(raw)
        .ok()
        .and_then(|url| url.host_str().map(str::to_string))
}

fn url_from_args(raw: &str) -> Option<String> {
    serde_json::from_str::<serde_json::Value>(raw)
        .ok()
        .and_then(|value| {
            value
                .get("url")
                .and_then(serde_json::Value::as_str)
                .map(str::to_string)
        })
}

fn result_is_error(result_meta: Option<&str>) -> bool {
    result_meta
        .and_then(|raw| serde_json::from_str::<serde_json::Value>(raw).ok())
        .and_then(|value| value.get("isError").and_then(serde_json::Value::as_bool))
        .unwrap_or(false)
}

fn safe_stringify(value: &serde_json::Value) -> String {
    serde_json::to_string(value).unwrap_or_else(|_| "\"<unserialisable>\"".to_string())
}

fn truncate(value: &str) -> String {
    if value.len() <= ARGS_JSON_MAX {
        value.to_string()
    } else {
        format!("{}~", &value[..ARGS_JSON_MAX - 1])
    }
}

fn summarize_result(result: &DispatchResultSummary) -> String {
    let structured_keys: Vec<String> = result
        .structured_content
        .as_object()
        .map(|obj| obj.keys().cloned().collect())
        .unwrap_or_default();
    let content_summary = result
        .content
        .as_array()
        .map(|items| format!("{} block(s)", items.len()))
        .unwrap_or_else(|| "unknown".to_string());
    json!({
        "isError": result.is_error,
        "contentSummary": content_summary,
        "structuredKeys": structured_keys,
    })
    .to_string()
}

#[cfg(test)]
mod tests {
    use super::{
        AuditService, DispatchResultSummary, ListTasksQuery, RecordToolDispatchInput, TaskStatus,
    };
    use rusqlite::{Connection, OptionalExtension};
    use serde_json::json;
    use std::path::Path;
    use tempfile::{TempDir, tempdir};

    fn dispatch(session_id: &str, url: &str, is_error: bool) -> RecordToolDispatchInput {
        RecordToolDispatchInput {
            agent_id: if session_id.starts_with("a") {
                "agent-a"
            } else {
                "agent-b"
            }
            .to_string(),
            slug: "agent".to_string(),
            agent_label: "Agent".to_string(),
            session_id: session_id.to_string(),
            tool_name: "navigate".to_string(),
            page_id: Some(1),
            target_id: Some("target".to_string()),
            url: Some(url.to_string()),
            title: None,
            raw_args: json!({ "url": url }),
            duration_ms: 10,
            dispatch_id: crate::domain::DispatchId::new(),
            result: DispatchResultSummary {
                is_error,
                structured_content: json!({ "page": 1 }),
                content: json!([{ "type": "text", "text": "ok" }]),
            },
        }
    }

    fn audit_path(dir: &TempDir) -> std::path::PathBuf {
        dir.path().join("audit.sqlite")
    }

    fn open_temp_audit(dir: &TempDir) -> anyhow::Result<Connection> {
        Ok(super::open_connection(audit_path(dir))?)
    }

    fn seed_ts_drizzle_schema(conn: &Connection) -> anyhow::Result<()> {
        conn.execute_batch(super::AUDIT_0000)?;
        conn.execute_batch(super::AUDIT_0001)?;
        super::seed_drizzle_migrations(conn)?;
        Ok(())
    }

    fn seed_legacy_rust_schema(path: &Path) -> anyhow::Result<()> {
        let conn = Connection::open(path)?;
        seed_ts_drizzle_schema(&conn)?;
        conn.execute_batch(super::AUDIT_0002)?;
        conn.execute_batch(super::AUDIT_0003)?;
        Ok(())
    }

    fn drizzle_entries(conn: &Connection) -> anyhow::Result<Vec<(String, i64)>> {
        if !super::table_exists(conn, "__drizzle_migrations")? {
            return Ok(Vec::new());
        }
        let mut stmt =
            conn.prepare("SELECT hash, created_at FROM __drizzle_migrations ORDER BY created_at")?;
        let rows = stmt
            .query_map([], |row| Ok((row.get(0)?, row.get(1)?)))?
            .collect::<Result<Vec<_>, _>>()?;
        Ok(rows)
    }

    fn expected_drizzle_entries() -> Vec<(String, i64)> {
        super::DRIZZLE_COMPAT_MIGRATIONS
            .iter()
            .map(|migration| (migration.tag.to_string(), migration.created_at))
            .collect()
    }

    fn index_exists(conn: &Connection, name: &str) -> anyhow::Result<bool> {
        let exists = conn
            .query_row(
                "SELECT 1 FROM sqlite_master WHERE type = 'index' AND name = ?1",
                [name],
                |_| Ok(()),
            )
            .optional()?
            .is_some();
        Ok(exists)
    }

    fn assert_current_schema(conn: &Connection) -> anyhow::Result<()> {
        assert_eq!(
            super::user_version(conn)?,
            super::CURRENT_AUDIT_SCHEMA_VERSION
        );
        assert!(super::table_exists(conn, "tool_dispatches")?);
        assert!(super::table_exists(conn, "agent_session_starts")?);
        assert!(super::table_exists(conn, "agent_session_ends")?);
        assert!(super::column_exists(
            conn,
            "tool_dispatches",
            "dispatch_id"
        )?);
        assert!(super::column_exists(
            conn,
            "tool_dispatches",
            "has_screenshot"
        )?);
        assert!(super::table_exists(conn, "tasks")?);
        assert!(index_exists(conn, "tasks_cursor_idx")?);
        assert!(index_exists(conn, "tasks_agent_cursor_idx")?);
        assert!(index_exists(conn, "tasks_status_cursor_idx")?);
        assert!(index_exists(conn, "tasks_site_cursor_idx")?);
        assert!(index_exists(conn, "tasks_started_idx")?);
        Ok(())
    }

    #[test]
    fn copied_drizzle_migrations_match_ts_sources() {
        assert_eq!(
            super::AUDIT_0000,
            include_str!("../../../claw-server/drizzle/0000_add_tool_dispatches.sql")
        );
        assert_eq!(
            super::AUDIT_0001,
            include_str!("../../../claw-server/drizzle/0001_add_agent_session_events.sql")
        );
    }

    #[test]
    fn fresh_db_runs_all_migrations_and_seeds_ts_compat_ledger() -> anyhow::Result<()> {
        let dir = tempdir()?;
        let conn = open_temp_audit(&dir)?;
        assert_current_schema(&conn)?;
        assert_eq!(drizzle_entries(&conn)?, expected_drizzle_entries());
        Ok(())
    }

    #[test]
    fn ts_drizzle_db_is_baselined_before_rust_migrations() -> anyhow::Result<()> {
        let dir = tempdir()?;
        let path = audit_path(&dir);
        let conn = Connection::open(&path)?;
        seed_ts_drizzle_schema(&conn)?;
        drop(conn);

        let conn = super::open_connection(path)?;
        assert_current_schema(&conn)?;
        assert_eq!(drizzle_entries(&conn)?, expected_drizzle_entries());
        Ok(())
    }

    #[test]
    fn legacy_rust_touched_db_is_baselined_without_rerunning() -> anyhow::Result<()> {
        let dir = tempdir()?;
        let path = audit_path(&dir);
        seed_legacy_rust_schema(&path)?;

        let conn = super::open_connection(path)?;
        assert_current_schema(&conn)?;
        assert_eq!(drizzle_entries(&conn)?, expected_drizzle_entries());
        Ok(())
    }

    #[test]
    fn migrations_are_idempotent_on_double_open() -> anyhow::Result<()> {
        let dir = tempdir()?;
        let path = audit_path(&dir);
        let conn = super::open_connection(path.clone())?;
        assert_current_schema(&conn)?;
        assert_eq!(drizzle_entries(&conn)?, expected_drizzle_entries());
        drop(conn);

        let conn = super::open_connection(path)?;
        assert_current_schema(&conn)?;
        assert_eq!(drizzle_entries(&conn)?, expected_drizzle_entries());
        Ok(())
    }

    #[tokio::test]
    async fn migrations_and_dispatch_pagination_work() -> anyhow::Result<()> {
        let dir = tempdir()?;
        let audit = AuditService::open(dir.path().join("audit.sqlite")).await?;
        assert!(
            audit
                .list_dispatches(Default::default())
                .await?
                .rows
                .is_empty()
        );
        for idx in 0..5 {
            let url = format!("https://example{idx}.com");
            audit
                .record_tool_dispatch(dispatch("a1", &url, false))
                .await?;
        }
        let first = audit
            .list_dispatches(super::ListDispatchesQuery {
                limit: Some(2),
                ..Default::default()
            })
            .await?;
        assert_eq!(first.rows.len(), 2);
        assert!(first.next_cursor.is_some());
        Ok(())
    }

    #[tokio::test]
    async fn task_filters_compose_before_pagination() -> anyhow::Result<()> {
        let dir = tempdir()?;
        let audit = AuditService::open(dir.path().join("audit.sqlite")).await?;
        audit
            .record_tool_dispatch(dispatch("a1", "https://alpha.example.com", false))
            .await?;
        audit.record_session_end("a1", "closed", None).await?;
        audit
            .record_tool_dispatch(dispatch("b1", "https://beta.example.com", true))
            .await?;
        let done = audit
            .list_tasks(ListTasksQuery {
                status: Some(TaskStatus::Done),
                search: Some("alpha".to_string()),
                limit: Some(1),
                ..Default::default()
            })
            .await?;
        assert_eq!(done.tasks.len(), 1);
        assert_eq!(done.tasks[0].session_id, "a1");
        assert_eq!(done.next_cursor, None);
        let failed = audit
            .list_tasks(ListTasksQuery {
                status: Some(TaskStatus::Failed),
                site: Some("beta.example.com".to_string()),
                ..Default::default()
            })
            .await?;
        assert_eq!(failed.tasks.len(), 1);
        assert_eq!(failed.tasks[0].session_id, "b1");
        Ok(())
    }
}
