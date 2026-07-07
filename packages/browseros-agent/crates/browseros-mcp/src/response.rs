use crate::{
    constants::TOOL_POST_ACTION_CAPTURE_TIMEOUT,
    format::{diff::format_diff_result, snapshot::format_snapshot_result},
    framework::{ToolCtx, ToolError, ToolExecResult, ToolResult, merge_structured},
};
use browseros_core::{PageId, settle::SettleOutcome};
use rmcp::model::ContentBlock;
use serde_json::{Value, json};

#[derive(Debug, Clone)]
enum PostAction {
    Snapshot { page: u32 },
    Diff { page: u32, include_structured: bool },
    Pages,
    Screenshot { page: u32 },
}

#[derive(Debug, Clone, Default)]
pub struct ToolResponse {
    content: Vec<ContentBlock>,
    has_error: bool,
    structured_content: Option<Value>,
    post_actions: Vec<PostAction>,
}

#[derive(Debug, Clone)]
pub struct BuiltToolResponse {
    pub content: Vec<ContentBlock>,
    pub is_error: bool,
    pub structured_content: Option<Value>,
    pub metadata_tab_id: Option<i64>,
}

impl ToolResponse {
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }

    pub fn text(&mut self, value: impl Into<String>) {
        self.content.push(ContentBlock::text(value));
    }

    pub fn image(&mut self, data: impl Into<String>, mime_type: impl Into<String>) {
        self.content.push(ContentBlock::image(data, mime_type));
    }

    pub fn error(&mut self, message: impl Into<String>) {
        self.has_error = true;
        self.text(message);
    }

    pub fn data(&mut self, value: Value) {
        merge_structured(&mut self.structured_content, value);
    }

    pub fn append_result(&mut self, result: ToolResult) {
        self.content.extend(result.content);
        if result.is_error {
            self.has_error = true;
        }
        if let Some(structured) = result.structured_content {
            self.data(structured);
        }
    }

    pub fn include_snapshot(&mut self, page: u32) {
        self.post_actions.push(PostAction::Snapshot { page });
    }

    pub fn include_diff(&mut self, page: u32, include_structured: bool) {
        self.post_actions.push(PostAction::Diff {
            page,
            include_structured,
        });
    }

    pub fn include_pages(&mut self) {
        self.post_actions.push(PostAction::Pages);
    }

    pub fn include_screenshot(&mut self, page: u32) {
        self.post_actions.push(PostAction::Screenshot { page });
    }

    pub async fn build_for_session(&mut self, ctx: &ToolCtx) -> ToolExecResult<BuiltToolResponse> {
        if !self.post_actions.is_empty() {
            self.text("\n--- Additional context (auto-included) ---");
        }

        for action in self.post_actions.clone() {
            ctx.throw_if_cancelled()?;
            self.settle_for_post_action(&action, ctx).await?;
            let result = tokio::select! {
                () = ctx.cancel.cancelled() => return Err(ToolError::Cancelled),
                result = tokio::time::timeout(
                    TOOL_POST_ACTION_CAPTURE_TIMEOUT,
                    self.run_session_post_action(action.clone(), ctx)
                ) => result,
            };
            match result {
                Ok(Ok(())) => {}
                Ok(Err(err)) => {
                    tracing::debug!("browser MCP post-action failed: {err}");
                    self.text(action.unavailable_text(err.to_string()));
                }
                Err(_elapsed) => {
                    let reason = format!(
                        "timed out after {}ms",
                        TOOL_POST_ACTION_CAPTURE_TIMEOUT.as_millis()
                    );
                    tracing::debug!(
                        "browser MCP post-action timed out: {}",
                        action.unavailable_text(&reason)
                    );
                    self.text(action.unavailable_text(reason));
                }
            }
        }

        Ok(BuiltToolResponse {
            content: self.content.clone(),
            is_error: self.has_error,
            structured_content: self.structured_content.clone(),
            metadata_tab_id: None,
        })
    }

    async fn run_session_post_action(
        &mut self,
        action: PostAction,
        ctx: &ToolCtx,
    ) -> ToolExecResult<()> {
        match action {
            PostAction::Snapshot { page } => {
                let observer = ctx.session.observe(browseros_core::PageId(page)).await;
                let snapshot = observer.snapshot().await?;
                let formatted = format_snapshot_result(&snapshot.text, &snapshot.url, ctx).await;
                self.text(format!("[Page {page} snapshot]\n{}", formatted.text));
                Ok(())
            }
            PostAction::Diff {
                page,
                include_structured,
            } => {
                let diff = ctx
                    .session
                    .observe(browseros_core::PageId(page))
                    .await
                    .diff()
                    .await?;
                let origin = diff.after_url.as_deref().map(ToString::to_string);
                let origin = match origin {
                    Some(origin) => origin,
                    None => ctx
                        .session
                        .pages
                        .get_info(browseros_core::PageId(page))
                        .await
                        .map(|info| info.url)
                        .unwrap_or_else(|| "unknown".to_string()),
                };
                let formatted = format_diff_result(&diff, &origin, ctx).await;
                self.text(format!("[Page {page} diff]\n{}", formatted.text));
                if include_structured {
                    let mut structured = json!({ "changed": diff.changed });
                    if diff.url_changed
                        && let Value::Object(object) = &mut structured
                    {
                        object.insert("urlChanged".to_string(), Value::Bool(true));
                        object.insert(
                            "beforeUrl".to_string(),
                            diff.before_url.map(Value::String).unwrap_or(Value::Null),
                        );
                        object.insert(
                            "afterUrl".to_string(),
                            diff.after_url.map(Value::String).unwrap_or(Value::Null),
                        );
                    }
                    self.data(structured);
                }
                Ok(())
            }
            PostAction::Pages => {
                let pages = ctx.session.pages.list().await?;
                if pages.is_empty() {
                    self.text("[Open pages] None");
                    return Ok(());
                }
                let lines = pages
                    .iter()
                    .map(|page| {
                        format!(
                            "  {}. {} - {}{}",
                            page.page_id.0,
                            if page.title.is_empty() {
                                "(untitled)"
                            } else {
                                &page.title
                            },
                            page.url,
                            if page.is_active { " [ACTIVE]" } else { "" }
                        )
                    })
                    .collect::<Vec<_>>();
                self.text(format!("[Open pages]\n{}", lines.join("\n")));
                Ok(())
            }
            PostAction::Screenshot { page } => {
                let page_session = ctx
                    .session
                    .pages
                    .get_session(browseros_core::PageId(page))
                    .await?;
                #[derive(serde::Deserialize)]
                struct CaptureScreenshotResult {
                    data: String,
                }
                let result: CaptureScreenshotResult = page_session
                    .session
                    .send(
                        "Page.captureScreenshot",
                        json!({ "format": "png", "captureBeyondViewport": false }),
                    )
                    .await?;
                self.text(format!("[Page {page} screenshot]"));
                self.image(result.data, "image/png");
                Ok(())
            }
        }
    }

    async fn settle_for_post_action(
        &self,
        action: &PostAction,
        ctx: &ToolCtx,
    ) -> ToolExecResult<()> {
        let Some(page) = action.settle_page() else {
            return Ok(());
        };
        let outcome = tokio::select! {
            () = ctx.cancel.cancelled() => return Err(ToolError::Cancelled),
            outcome = browseros_core::settle::wait_for_action_settle(
                ctx.session.pages.as_ref(),
                PageId(page),
                browseros_core::timeouts::ACTION_SETTLE_DEFAULT_TIMEOUT,
            ) => outcome,
        };
        match outcome {
            SettleOutcome::Settled { .. } => {}
            SettleOutcome::BudgetExpired => {
                tracing::debug!("browser MCP post-action settle budget expired for page {page}");
            }
            SettleOutcome::Skipped { reason } => {
                tracing::debug!("browser MCP post-action settle skipped for page {page}: {reason}");
            }
        }
        Ok(())
    }
}

impl BuiltToolResponse {
    #[must_use]
    pub fn into_tool_result(self) -> ToolResult {
        let _ = self.metadata_tab_id;
        ToolResult {
            content: self.content,
            is_error: self.is_error,
            structured_content: self.structured_content,
        }
    }
}

impl PostAction {
    fn settle_page(&self) -> Option<u32> {
        match self {
            Self::Snapshot { page } | Self::Diff { page, .. } => Some(*page),
            Self::Pages | Self::Screenshot { .. } => None,
        }
    }

    fn unavailable_text(&self, reason: impl AsRef<str>) -> String {
        let reason = reason.as_ref();
        match self {
            Self::Snapshot { page } => format!("[page {page} snapshot unavailable: {reason}]"),
            Self::Diff { page, .. } => format!("[page {page} diff unavailable: {reason}]"),
            Self::Pages => format!("[pages unavailable: {reason}]"),
            Self::Screenshot { page } => format!("[page {page} screenshot unavailable: {reason}]"),
        }
    }
}
