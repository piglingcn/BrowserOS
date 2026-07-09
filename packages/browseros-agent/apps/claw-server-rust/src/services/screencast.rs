use crate::services::{
    browser::BrowserService,
    now_epoch_ms,
    tab_activity::{ScreencastFrame, TabActivityService},
};
use browseros_core::{
    PageId,
    screenshot::{ScreenshotCaptureOptions, ScreenshotFormat},
};
use std::{
    collections::{HashMap, VecDeque},
    sync::Arc,
    time::Duration,
};
use tokio::{
    sync::Mutex,
    task::JoinHandle,
    time::{MissedTickBehavior, interval},
};
use tokio_util::sync::CancellationToken;
use tracing::warn;

const POLL_INTERVAL: Duration = Duration::from_millis(1500);
const FAILURE_BACKOFF_MS: i64 = 5_000;
const FAILURE_BACKOFF_THRESHOLD: u8 = 3;

#[derive(Clone)]
pub struct ScreencastService {
    inner: Arc<Mutex<ScreencastInner>>,
    cancel: CancellationToken,
    capacity: usize,
}

#[derive(Default)]
struct ScreencastInner {
    frames: HashMap<u32, ScreencastFrame>,
    order: VecDeque<u32>,
    failures: HashMap<u32, u8>,
    retry_after: HashMap<u32, i64>,
}

impl ScreencastService {
    #[must_use]
    pub fn new(capacity: usize) -> Arc<Self> {
        Arc::new(Self {
            inner: Arc::new(Mutex::new(ScreencastInner::default())),
            cancel: CancellationToken::new(),
            capacity,
        })
    }

    pub fn start(
        self: Arc<Self>,
        browser: Arc<BrowserService>,
        tab_activity: Arc<TabActivityService>,
    ) -> JoinHandle<()> {
        tokio::spawn(async move {
            let mut ticker = interval(POLL_INTERVAL);
            ticker.set_missed_tick_behavior(MissedTickBehavior::Delay);
            loop {
                tokio::select! {
                    () = self.cancel.cancelled() => return,
                    _ = ticker.tick() => {
                        self.capture_active_pages(browser.clone(), tab_activity.clone()).await;
                    }
                }
            }
        })
    }

    pub fn stop(&self) {
        self.cancel.cancel();
    }

    pub async fn frame_for(&self, page_id: u32) -> Option<ScreencastFrame> {
        self.inner.lock().await.frames.get(&page_id).cloned()
    }

    async fn capture_active_pages(
        &self,
        browser: Arc<BrowserService>,
        tab_activity: Arc<TabActivityService>,
    ) {
        let Some(session) = browser.session().await else {
            return;
        };
        let pages = tab_activity.snapshot().await;
        for record in pages.into_iter().filter(|record| record.status == "active") {
            if self.is_backing_off(record.page_id).await {
                continue;
            }
            let options = ScreenshotCaptureOptions {
                format: Some(ScreenshotFormat::Jpeg),
                quality: Some(50),
                full_page: Some(false),
                annotate: Some(false),
                clip: None,
            };
            match session.screenshot(PageId(record.page_id), options).await {
                Ok(capture) => {
                    self.store_frame(
                        record.page_id,
                        ScreencastFrame {
                            jpeg_base64: capture.data,
                            captured_at: now_epoch_ms(),
                        },
                    )
                    .await;
                }
                Err(err) => {
                    warn!(page_id = record.page_id, error = %err, "screencast capture failed");
                    self.record_failure(record.page_id).await;
                }
            }
        }
    }

    async fn is_backing_off(&self, page_id: u32) -> bool {
        self.inner
            .lock()
            .await
            .retry_after
            .get(&page_id)
            .copied()
            .map(|retry_after| now_epoch_ms() < retry_after)
            .unwrap_or(false)
    }

    async fn store_frame(&self, page_id: u32, frame: ScreencastFrame) {
        let mut inner = self.inner.lock().await;
        inner.frames.insert(page_id, frame);
        inner.failures.remove(&page_id);
        inner.retry_after.remove(&page_id);
        if let Some(pos) = inner.order.iter().position(|existing| *existing == page_id) {
            inner.order.remove(pos);
        }
        inner.order.push_back(page_id);
        while inner.order.len() > self.capacity {
            if let Some(evicted) = inner.order.pop_front() {
                inner.frames.remove(&evicted);
                inner.failures.remove(&evicted);
                inner.retry_after.remove(&evicted);
            }
        }
    }

    async fn record_failure(&self, page_id: u32) {
        let mut inner = self.inner.lock().await;
        let failures = inner.failures.entry(page_id).or_insert(0);
        *failures = failures.saturating_add(1);
        if *failures >= FAILURE_BACKOFF_THRESHOLD {
            inner
                .retry_after
                .insert(page_id, now_epoch_ms().saturating_add(FAILURE_BACKOFF_MS));
        }
    }
}

#[cfg(test)]
mod tests {
    use super::ScreencastService;
    use crate::services::tab_activity::ScreencastFrame;

    #[tokio::test]
    async fn frame_cache_is_lru_capped() {
        let service = ScreencastService::new(2);
        service
            .store_frame(
                1,
                ScreencastFrame {
                    jpeg_base64: "a".to_string(),
                    captured_at: 1,
                },
            )
            .await;
        service
            .store_frame(
                2,
                ScreencastFrame {
                    jpeg_base64: "b".to_string(),
                    captured_at: 2,
                },
            )
            .await;
        service
            .store_frame(
                3,
                ScreencastFrame {
                    jpeg_base64: "c".to_string(),
                    captured_at: 3,
                },
            )
            .await;
        assert!(service.frame_for(1).await.is_none());
        assert!(service.frame_for(2).await.is_some());
        assert!(service.frame_for(3).await.is_some());
    }
}
