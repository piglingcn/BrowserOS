use crate::{
    AppState,
    domain::SessionId,
    error::{AppError, AppResult},
    services::{
        agents::{Harness, NewAgentValues},
        audit::{ListDispatchesQuery, ListTasksQuery, TaskStatus},
        replay::ReplayService,
        site_rules::AddSiteRule,
        tab_activity::EnrichedTabRecord,
    },
};
use axum::{
    Json, Router,
    body::Body,
    extract::{Path, Query, Request, State, rejection::JsonRejection},
    http::{HeaderValue, Method, StatusCode, header},
    middleware::Next,
    response::{IntoResponse, Response},
    routing::{any, delete, get, options, post},
};
use serde::Deserialize;
use serde_json::{Value, json};
use std::str::FromStr;
use tracing::{Instrument, info_span};
use ulid::Ulid;

pub fn router() -> Router<AppState> {
    Router::new()
        .route("/system/health", get(system_health))
        .route("/system/shutdown", post(system_shutdown))
        .route("/system/version", get(system_version))
        .route("/system/url", get(system_url))
        .route("/agents", post(agents_create).get(agents_list))
        .route(
            "/agents/{id}",
            get(agents_detail)
                .patch(agents_update)
                .delete(agents_delete),
        )
        .route("/agents/{id}/mcp-url:regenerate", post(agents_regenerate))
        .route("/agents/{agent_id}/cancel", post(agents_cancel))
        .route("/site-rules", get(site_rules_list).post(site_rules_create))
        .route("/site-rules/{id}", delete(site_rules_delete))
        .route("/permissions/catalog", get(permissions_catalog))
        .route("/tabs/activity", get(tabs_activity))
        .route("/tabs/focus/{agent_id}", post(tabs_focus))
        .route("/connections", get(connections_list))
        .route("/connections/{harness}/connect", post(connections_connect))
        .route(
            "/connections/{harness}/disconnect",
            post(connections_disconnect),
        )
        .route("/audit/dispatches", get(audit_dispatches))
        .route("/audit/tasks", get(audit_tasks))
        .route("/audit/tasks/{session_id}", get(audit_task_detail))
        .route("/audit/screenshot/{dispatch_id}", get(audit_screenshot))
        .route(
            "/audit/replay/{session_id}/events",
            post(replay_post_events),
        )
        .route("/audit/replay/{session_id}", get(replay_get))
        .route("/audit/replay/{session_id}/exists", get(replay_exists))
        .route("/replay/tabs", get(replay_tabs))
        .route("/mcp", any(mcp_stub))
        .route("/{*path}", options(preflight))
}

pub async fn request_context(req: Request, next: Next) -> Response {
    let request_id = Ulid::new().to_string();
    let method = req.method().clone();
    let path = req.uri().path().to_string();
    let span = info_span!("http_request", %request_id, %method, %path);
    async move {
        let mut response = next.run(req).await;
        let headers = response.headers_mut();
        headers.insert(
            header::ACCESS_CONTROL_ALLOW_ORIGIN,
            HeaderValue::from_static("*"),
        );
        headers.insert(
            header::ACCESS_CONTROL_ALLOW_METHODS,
            HeaderValue::from_static("GET,POST,PATCH,DELETE,OPTIONS"),
        );
        headers.insert(
            header::ACCESS_CONTROL_ALLOW_HEADERS,
            HeaderValue::from_static("content-type,authorization,mcp-session-id"),
        );
        if let Ok(value) = HeaderValue::from_str(&request_id) {
            headers.insert("x-request-id", value);
        }
        response
    }
    .instrument(span)
    .await
}

async fn preflight() -> StatusCode {
    StatusCode::NO_CONTENT
}

async fn system_health(State(state): State<AppState>) -> Json<Value> {
    let cdp = state.browser.state();
    Json(json!({
        "status": "ok",
        "cdp": cdp,
        "sessions": {
            "count": state.sessions.count().await
        }
    }))
}

async fn system_shutdown(State(state): State<AppState>) -> AppResult<Json<Value>> {
    let drained = state.sessions.shutdown().await?;
    state.browser.stop();
    if let Some(tx) = state.shutdown.lock().await.take() {
        let _ = tx.send(());
    }
    Ok(Json(json!({ "status": "ok", "drainedSessions": drained })))
}

async fn system_version() -> Json<Value> {
    Json(json!({
        "name": env!("CARGO_PKG_NAME"),
        "version": env!("CARGO_PKG_VERSION"),
    }))
}

async fn system_url(State(state): State<AppState>) -> Json<Value> {
    Json(json!({ "url": state.config.local_server_url() }))
}

async fn agents_create(
    State(state): State<AppState>,
    payload: Result<Json<NewAgentValues>, JsonRejection>,
) -> AppResult<impl IntoResponse> {
    let Json(body) = json_body(payload)?;
    let created = state.agents.create(body).await?;
    Ok((StatusCode::CREATED, Json(created)))
}

async fn agents_list(State(state): State<AppState>) -> AppResult<Json<Value>> {
    Ok(Json(serde_json::to_value(state.agents.list().await?)?))
}

async fn agents_detail(
    State(state): State<AppState>,
    Path(id): Path<String>,
) -> AppResult<Json<Value>> {
    let detail = state
        .agents
        .get_detail(&id)
        .await?
        .ok_or_else(|| AppError::not_found("agent not found"))?;
    Ok(Json(serde_json::to_value(detail)?))
}

async fn agents_update(
    State(state): State<AppState>,
    Path(id): Path<String>,
    payload: Result<Json<NewAgentValues>, JsonRejection>,
) -> AppResult<Json<Value>> {
    let Json(body) = json_body(payload)?;
    let updated = state
        .agents
        .update(&id, body)
        .await?
        .ok_or_else(|| AppError::not_found("agent not found"))?;
    Ok(Json(serde_json::to_value(updated)?))
}

async fn agents_delete(
    State(state): State<AppState>,
    Path(id): Path<String>,
) -> AppResult<Json<Value>> {
    let deleted = state
        .agents
        .remove(&id)
        .await?
        .ok_or_else(|| AppError::not_found("agent not found"))?;
    Ok(Json(serde_json::to_value(deleted)?))
}

async fn agents_regenerate(
    State(state): State<AppState>,
    Path(id): Path<String>,
) -> AppResult<Json<Value>> {
    let rotated = state
        .agents
        .regenerate_mcp_url(&id)
        .await?
        .ok_or_else(|| AppError::not_found("agent not found"))?;
    Ok(Json(serde_json::to_value(rotated)?))
}

async fn agents_cancel(
    State(state): State<AppState>,
    Path(agent_id): Path<String>,
) -> AppResult<Json<Value>> {
    let cancelled = state.sessions.cancel_by_agent(&agent_id).await;
    if cancelled == 0 {
        return Err(AppError::not_found("no active dispatches for this agent"));
    }
    Ok(Json(json!({ "ok": true, "cancelled": cancelled })))
}

async fn site_rules_list(State(state): State<AppState>) -> AppResult<Json<Value>> {
    Ok(Json(serde_json::to_value(state.site_rules.list().await?)?))
}

async fn site_rules_create(
    State(state): State<AppState>,
    payload: Result<Json<AddSiteRule>, JsonRejection>,
) -> AppResult<impl IntoResponse> {
    let Json(body) = json_body(payload)?;
    let created = state.site_rules.add(body).await?;
    Ok((StatusCode::CREATED, Json(created)))
}

async fn site_rules_delete(
    State(state): State<AppState>,
    Path(id): Path<String>,
) -> AppResult<Json<Value>> {
    let deleted = state
        .site_rules
        .remove(&id)
        .await?
        .ok_or_else(|| AppError::not_found("site rule not found"))?;
    Ok(Json(deleted))
}

async fn permissions_catalog() -> Json<Value> {
    Json(json!([
        { "id": "submit", "name": "Submit / send / post", "defaultVerdict": "Ask", "allowAuto": true },
        { "id": "payment", "name": "Payments & checkout", "defaultVerdict": "Block", "allowAuto": false },
        { "id": "delete", "name": "Delete / destructive", "defaultVerdict": "Ask", "allowAuto": true },
        { "id": "upload", "name": "File upload", "defaultVerdict": "Ask", "allowAuto": true },
        { "id": "navigate", "name": "Navigate to a new site", "defaultVerdict": "Ask", "allowAuto": true },
        { "id": "input", "name": "Click & type", "defaultVerdict": "Auto", "allowAuto": true }
    ]))
}

async fn tabs_activity(State(state): State<AppState>) -> AppResult<Json<Value>> {
    let profiles = state.agents.list().await?;
    let tabs = state.tab_activity.snapshot().await;
    let enriched: Vec<EnrichedTabRecord> = tabs
        .into_iter()
        .map(|record| {
            let profile = profiles.iter().find(|profile| {
                profile.id == record.agent_id || profile.mcp_url.contains(&record.slug)
            });
            EnrichedTabRecord {
                agent_label: profile
                    .map(|profile| profile.name.clone())
                    .unwrap_or_else(|| record.slug.clone()),
                harness: profile.map(|profile| profile.harness.to_string()),
                color: None,
                screencast: None,
                record,
            }
        })
        .collect();
    Ok(Json(json!({ "tabs": enriched })))
}

async fn tabs_focus(
    State(state): State<AppState>,
    Path(agent_id): Path<String>,
) -> AppResult<Response> {
    if state.browser.session().await.is_none() {
        return Ok((
            StatusCode::SERVICE_UNAVAILABLE,
            Json(json!({ "ok": false, "reason": "browser session not connected" })),
        )
            .into_response());
    }
    Ok((
        StatusCode::NOT_FOUND,
        Json(json!({
            "ok": false,
            "reason": format!("no tab group registered for {agent_id}")
        })),
    )
        .into_response())
}

async fn connections_list(State(state): State<AppState>) -> AppResult<Json<Value>> {
    Ok(Json(json!({
        "connections": state.harness.list_browseros_connections().await?
    })))
}

async fn connections_connect(
    State(state): State<AppState>,
    Path(harness): Path<String>,
) -> AppResult<Json<Value>> {
    let harness = Harness::from_str(&harness)?;
    let result = state
        .harness
        .connect_browseros(harness, &state.config.public_mcp_url())
        .await?;
    Ok(Json(serde_json::to_value(result)?))
}

async fn connections_disconnect(
    State(state): State<AppState>,
    Path(harness): Path<String>,
) -> AppResult<Json<Value>> {
    let harness = Harness::from_str(&harness)?;
    let result = state.harness.disconnect_browseros(harness).await?;
    Ok(Json(serde_json::to_value(result)?))
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
struct DispatchesQuery {
    agent_id: Option<String>,
    session_id: Option<String>,
    cursor: Option<i64>,
    limit: Option<i64>,
}

async fn audit_dispatches(
    State(state): State<AppState>,
    Query(query): Query<DispatchesQuery>,
) -> AppResult<Json<Value>> {
    validate_limit(query.limit, 500)?;
    let result = state
        .audit
        .list_dispatches(ListDispatchesQuery {
            agent_id: query.agent_id,
            session_id: query.session_id,
            cursor: query.cursor,
            limit: query.limit,
        })
        .await?;
    Ok(Json(serde_json::to_value(result)?))
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
struct TasksQuery {
    agent_id: Option<String>,
    status: Option<TaskStatus>,
    site: Option<String>,
    search: Option<String>,
    since: Option<i64>,
    cursor: Option<i64>,
    limit: Option<i64>,
}

async fn audit_tasks(
    State(state): State<AppState>,
    Query(query): Query<TasksQuery>,
) -> AppResult<Json<Value>> {
    validate_limit(query.limit, 100)?;
    let result = state
        .audit
        .list_tasks(ListTasksQuery {
            agent_id: query.agent_id,
            status: query.status,
            site: query.site,
            search: query.search,
            since: query.since,
            cursor: query.cursor,
            limit: query.limit,
        })
        .await?;
    Ok(Json(serde_json::to_value(result)?))
}

async fn audit_task_detail(
    State(state): State<AppState>,
    Path(session_id): Path<String>,
) -> AppResult<Json<Value>> {
    let task = state
        .audit
        .get_task(&session_id)
        .await?
        .ok_or_else(|| AppError::not_found("not found"))?;
    Ok(Json(serde_json::to_value(task)?))
}

async fn audit_screenshot(
    State(state): State<AppState>,
    Path(dispatch_id): Path<String>,
) -> AppResult<impl IntoResponse> {
    let bytes = state.screenshots.read(&dispatch_id).await?;
    Ok((
        [
            (header::CONTENT_TYPE, HeaderValue::from_static("image/jpeg")),
            (
                header::CACHE_CONTROL,
                HeaderValue::from_static("public, max-age=86400, immutable"),
            ),
        ],
        bytes,
    ))
}

async fn replay_post_events(
    State(state): State<AppState>,
    Path(session_id): Path<String>,
    body: String,
) -> AppResult<Json<Value>> {
    if !state
        .sessions
        .contains(&SessionId::new(session_id.clone()))
        .await
    {
        return Err(AppError::gone("session not live"));
    }
    if body.is_empty() {
        return Ok(Json(json!({ "ok": true, "accepted": 0 })));
    }
    let lines: Vec<String> = body
        .lines()
        .filter(|line| !line.is_empty())
        .map(|line| ReplayService::annotate_with_session_id(line, &session_id))
        .collect();
    let accepted = lines.len();
    state.replay.append_events(&session_id, &lines).await?;
    Ok(Json(json!({ "ok": true, "accepted": accepted })))
}

async fn replay_get(
    State(state): State<AppState>,
    Path(session_id): Path<String>,
) -> AppResult<Response> {
    let stat = state.replay.stat_session(&session_id).await?;
    if !stat.has_data {
        return Err(AppError::not_found("no replay for this session"));
    }
    let bytes = state.replay.read_events(&session_id).await?;
    let mut response = Response::new(Body::from(bytes));
    let headers = response.headers_mut();
    headers.insert(
        header::CONTENT_TYPE,
        HeaderValue::from_static("application/x-ndjson"),
    );
    if let Ok(value) = HeaderValue::from_str(&stat.size_bytes.to_string()) {
        headers.insert(header::CONTENT_LENGTH, value);
    }
    headers.insert(header::ACCEPT_RANGES, HeaderValue::from_static("bytes"));
    Ok(response)
}

async fn replay_exists(
    State(state): State<AppState>,
    Path(session_id): Path<String>,
) -> AppResult<Json<Value>> {
    let stat = state.replay.stat_session(&session_id).await?;
    let mut value = serde_json::to_value(stat)?;
    if let Value::Object(obj) = &mut value {
        obj.insert("ok".to_string(), Value::Bool(true));
    }
    Ok(Json(value))
}

async fn replay_tabs() -> Json<Value> {
    Json(json!({ "tabs": [] }))
}

async fn mcp_stub() -> impl IntoResponse {
    (
        StatusCode::SERVICE_UNAVAILABLE,
        Json(json!({ "error": "mcp endpoint lands in phase D" })),
    )
}

fn json_body<T>(payload: Result<Json<T>, JsonRejection>) -> AppResult<Json<T>> {
    payload.map_err(|err| AppError::bad_request(err.body_text()))
}

fn validate_limit(limit: Option<i64>, cap: i64) -> AppResult<()> {
    if let Some(limit) = limit
        && (limit <= 0 || limit > cap)
    {
        return Err(AppError::bad_request("limit out of range"));
    }
    Ok(())
}

#[allow(dead_code)]
fn _method(_: Method) {}
