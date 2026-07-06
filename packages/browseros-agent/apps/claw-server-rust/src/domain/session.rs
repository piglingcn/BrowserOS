use crate::domain::{AgentRef, SessionId};
use browseros_core::PageId;
use std::{collections::BTreeSet, sync::Arc, time::Duration};
use tokio::{
    sync::{Mutex, RwLock},
    time::Instant,
};
use tokio_util::sync::CancellationToken;

pub struct Session {
    id: SessionId,
    agent: AgentRef,
    owned_pages: RwLock<BTreeSet<PageId>>,
    cancel: CancellationToken,
    tab_group_ref: Mutex<Option<String>>,
    replay_handle: Mutex<Option<String>>,
    last_activity: Mutex<Instant>,
}

impl Session {
    #[must_use]
    pub fn new(id: SessionId, agent: AgentRef, now: Instant) -> Arc<Self> {
        Arc::new(Self {
            id,
            agent,
            owned_pages: RwLock::new(BTreeSet::new()),
            cancel: CancellationToken::new(),
            tab_group_ref: Mutex::new(None),
            replay_handle: Mutex::new(None),
            last_activity: Mutex::new(now),
        })
    }

    #[must_use]
    pub fn id(&self) -> &SessionId {
        &self.id
    }

    #[must_use]
    pub fn agent(&self) -> &AgentRef {
        &self.agent
    }

    pub async fn touch(&self, now: Instant) {
        *self.last_activity.lock().await = now;
    }

    pub async fn idle_for(&self, now: Instant) -> Duration {
        now.saturating_duration_since(*self.last_activity.lock().await)
    }

    pub async fn add_owned_page(&self, page_id: PageId) {
        self.owned_pages.write().await.insert(page_id);
    }

    pub async fn owned_pages(&self) -> Vec<PageId> {
        self.owned_pages.read().await.iter().cloned().collect()
    }

    pub async fn set_tab_group_ref(&self, value: Option<String>) {
        *self.tab_group_ref.lock().await = value;
    }

    pub async fn set_replay_handle(&self, value: Option<String>) {
        *self.replay_handle.lock().await = value;
    }

    pub fn cancel(&self) {
        self.cancel.cancel();
    }

    #[must_use]
    pub fn child_token(&self) -> CancellationToken {
        self.cancel.child_token()
    }
}
