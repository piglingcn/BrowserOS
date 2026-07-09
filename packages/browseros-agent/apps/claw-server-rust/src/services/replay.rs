use crate::error::{AppError, AppResult, IoPath};
use serde::Serialize;
use serde_json::Value;
use std::{
    collections::{BTreeSet, HashMap, VecDeque},
    path::{Path, PathBuf},
    sync::Arc,
    time::{Duration, Instant},
};
use tokio::{fs, io::AsyncWriteExt, sync::Mutex};

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ReplayMetadata {
    pub has_data: bool,
    pub size_bytes: u64,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub first_event_at: Option<i64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub last_event_at: Option<i64>,
    pub tab_page_ids: Vec<i64>,
}

pub struct ReplayService {
    root: PathBuf,
    max_open_handles: usize,
    idle_handle: Duration,
    inner: Mutex<ReplayInner>,
}

struct ReplayInner {
    locks: HashMap<String, Arc<Mutex<()>>>,
    lru: VecDeque<(String, Instant)>,
}

impl ReplayService {
    #[must_use]
    pub fn new(root: PathBuf, max_open_handles: usize, idle_handle: Duration) -> Self {
        Self {
            root,
            max_open_handles,
            idle_handle,
            inner: Mutex::new(ReplayInner {
                locks: HashMap::new(),
                lru: VecDeque::new(),
            }),
        }
    }

    pub async fn append_events(&self, session_id: &str, lines: &[String]) -> AppResult<()> {
        if lines.is_empty() {
            return Ok(());
        }
        let lock = self.lock_for(session_id).await;
        let _guard = lock.lock().await;
        let path = self.path_for(session_id);
        if let Some(parent) = path.parent() {
            fs::create_dir_all(parent).await.with_path(parent)?;
        }
        let mut file = fs::OpenOptions::new()
            .create(true)
            .append(true)
            .open(&path)
            .await
            .with_path(path.clone())?;
        let mut payload = lines.join("\n");
        payload.push('\n');
        file.write_all(payload.as_bytes())
            .await
            .with_path(path.clone())?;
        file.flush().await.with_path(path)?;
        self.bump_lru(session_id).await;
        Ok(())
    }

    pub async fn read_events(&self, session_id: &str) -> AppResult<Vec<u8>> {
        let path = self.path_for(session_id);
        match fs::read(&path).await {
            Ok(bytes) => Ok(bytes),
            Err(err) if err.kind() == std::io::ErrorKind::NotFound => Ok(Vec::new()),
            Err(source) => Err(AppError::Io {
                path: Some(path),
                source,
            }),
        }
    }

    pub async fn stat_session(&self, session_id: &str) -> AppResult<ReplayMetadata> {
        let path = self.path_for(session_id);
        let meta = match fs::metadata(&path).await {
            Ok(meta) => meta,
            Err(err) if err.kind() == std::io::ErrorKind::NotFound => return Ok(empty_meta()),
            Err(source) => {
                return Err(AppError::Io {
                    path: Some(path),
                    source,
                });
            }
        };
        if meta.len() == 0 {
            return Ok(empty_meta());
        }
        let raw = fs::read_to_string(&path).await.with_path(path)?;
        let mut first_event_at = None;
        let mut last_event_at = None;
        let mut tab_page_ids = BTreeSet::new();
        for line in raw.lines().filter(|line| !line.trim().is_empty()) {
            if let Ok(value) = serde_json::from_str::<Value>(line) {
                if first_event_at.is_none() {
                    first_event_at = value.get("ts").and_then(Value::as_i64);
                }
                if let Some(ts) = value.get("ts").and_then(Value::as_i64) {
                    last_event_at = Some(ts);
                }
                if let Some(id) = value.get("tabPageId").and_then(Value::as_i64) {
                    tab_page_ids.insert(id);
                }
            }
        }
        Ok(ReplayMetadata {
            has_data: true,
            size_bytes: meta.len(),
            first_event_at,
            last_event_at,
            tab_page_ids: tab_page_ids.into_iter().collect(),
        })
    }

    pub async fn delete_session(&self, session_id: &str) -> AppResult<()> {
        let path = self.path_for(session_id);
        match fs::remove_file(&path).await {
            Ok(()) => Ok(()),
            Err(err) if err.kind() == std::io::ErrorKind::NotFound => Ok(()),
            Err(source) => Err(AppError::Io {
                path: Some(path),
                source,
            }),
        }
    }

    pub async fn close_session(&self, session_id: &str) -> AppResult<()> {
        let mut inner = self.inner.lock().await;
        inner.locks.remove(&sanitize_session_id(session_id));
        inner
            .lru
            .retain(|(id, _)| id != &sanitize_session_id(session_id));
        Ok(())
    }

    #[must_use]
    pub fn annotate_with_session_id(line: &str, session_id: &str) -> String {
        match serde_json::from_str::<Value>(line) {
            Ok(Value::Object(mut obj)) => {
                obj.insert(
                    "sessionId".to_string(),
                    Value::String(session_id.to_string()),
                );
                Value::Object(obj).to_string()
            }
            _ => line.to_string(),
        }
    }

    async fn lock_for(&self, session_id: &str) -> Arc<Mutex<()>> {
        let key = sanitize_session_id(session_id);
        let mut inner = self.inner.lock().await;
        inner
            .locks
            .entry(key)
            .or_insert_with(|| Arc::new(Mutex::new(())))
            .clone()
    }

    async fn bump_lru(&self, session_id: &str) {
        let key = sanitize_session_id(session_id);
        let mut inner = self.inner.lock().await;
        let now = Instant::now();
        inner.lru.retain(|(id, at)| {
            id != &key && now.saturating_duration_since(*at) <= self.idle_handle
        });
        inner.lru.push_back((key.clone(), now));
        while inner.lru.len() > self.max_open_handles {
            if let Some((old, _)) = inner.lru.pop_front() {
                inner.locks.remove(&old);
            }
        }
    }

    fn path_for(&self, session_id: &str) -> PathBuf {
        self.root
            .join(format!("{}.ndjson", sanitize_session_id(session_id)))
    }
}

fn empty_meta() -> ReplayMetadata {
    ReplayMetadata {
        has_data: false,
        size_bytes: 0,
        first_event_at: None,
        last_event_at: None,
        tab_page_ids: Vec::new(),
    }
}

fn sanitize_session_id(session_id: &str) -> String {
    session_id
        .chars()
        .map(|ch| {
            if ch.is_ascii_alphanumeric() || ch == '.' || ch == '_' || ch == '-' {
                ch
            } else {
                '_'
            }
        })
        .collect()
}

#[allow(dead_code)]
fn _assert_path(_: &Path) {}

#[cfg(test)]
mod tests {
    use super::ReplayService;
    use std::time::Duration;
    use tempfile::tempdir;

    #[tokio::test]
    async fn appends_and_stats_replay_lines() -> anyhow::Result<()> {
        let dir = tempdir()?;
        let replay = ReplayService::new(dir.path().to_path_buf(), 50, Duration::from_secs(30));
        let lines = vec![
            ReplayService::annotate_with_session_id(r#"{"tabPageId":1,"ts":100}"#, "s1"),
            ReplayService::annotate_with_session_id(r#"{"tabPageId":2,"ts":200}"#, "s1"),
        ];
        replay.append_events("s1", &lines).await?;
        let meta = replay.stat_session("s1").await?;
        assert!(meta.has_data);
        assert_eq!(meta.first_event_at, Some(100));
        assert_eq!(meta.last_event_at, Some(200));
        assert_eq!(meta.tab_page_ids, vec![1, 2]);
        let text = String::from_utf8(replay.read_events("s1").await?)?;
        assert!(text.contains(r#""sessionId":"s1""#));
        Ok(())
    }

    #[tokio::test]
    async fn unknown_replay_has_empty_metadata() -> anyhow::Result<()> {
        let dir = tempdir()?;
        let replay = ReplayService::new(dir.path().to_path_buf(), 50, Duration::from_secs(30));
        let meta = replay.stat_session("missing").await?;
        assert!(!meta.has_data);
        assert_eq!(meta.size_bytes, 0);
        Ok(())
    }
}
