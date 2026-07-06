use axum::{
    Router,
    body::{Body, to_bytes},
    http::{Request, StatusCode, header},
};
use claw_server_rust::{AppState, build_router, config::Config};
use serde_json::{Value, json};
use std::{sync::Arc, time::Duration};
use tempfile::TempDir;
use tower::ServiceExt;

struct TestApp {
    router: Router,
    _dir: TempDir,
}

async fn test_app() -> anyhow::Result<TestApp> {
    let dir = tempfile::tempdir()?;
    let root = dir.path().join("browseros");
    let config = Arc::new(Config {
        server_port: 9200,
        cdp_port: 49337,
        proxy_port: None,
        resources_dir: dir.path().join("resources"),
        browseros_dir: root.clone(),
        claw_dir: root.join("claw-server"),
        session_idle: Duration::from_secs(300),
        session_sweep_interval: Duration::from_secs(60),
        screencast_screenshot_fallback: true,
        dev_mode: false,
        auth_token: None,
    });
    let state = AppState::new_with_home(config, None, dir.path().join("home")).await?;
    Ok(TestApp {
        router: build_router(state),
        _dir: dir,
    })
}

async fn request_json(
    router: &Router,
    method: &str,
    uri: &str,
    body: Option<Value>,
) -> anyhow::Result<(StatusCode, Value)> {
    let mut builder = Request::builder().method(method).uri(uri);
    let request_body = if let Some(body) = body {
        builder = builder.header(header::CONTENT_TYPE, "application/json");
        Body::from(body.to_string())
    } else {
        Body::empty()
    };
    let response = router.clone().oneshot(builder.body(request_body)?).await?;
    let status = response.status();
    let bytes = to_bytes(response.into_body(), usize::MAX).await?;
    let value = if bytes.is_empty() {
        Value::Null
    } else {
        serde_json::from_slice(&bytes)?
    };
    Ok((status, value))
}

fn agent_body(name: &str) -> Value {
    json!({
        "name": name,
        "harness": "Codex",
        "loginMode": "profile",
        "selectedSites": [],
        "approvals": {
            "submit": "Ask",
            "payment": "Block",
            "delete": "Ask",
            "upload": "Ask",
            "navigate": "Ask",
            "input": "Auto"
        },
        "aclRuleIds": [],
        "customAclRules": []
    })
}

#[tokio::test]
async fn health_survives_cdp_down() -> anyhow::Result<()> {
    let app = test_app().await?;
    let (status, body) = request_json(&app.router, "GET", "/system/health", None).await?;
    assert_eq!(status, StatusCode::OK);
    assert_eq!(body["status"], "ok");
    assert_eq!(body["cdp"]["connected"], false);
    Ok(())
}

#[tokio::test]
async fn agents_crud_roundtrip() -> anyhow::Result<()> {
    let app = test_app().await?;
    let (status, created) = request_json(
        &app.router,
        "POST",
        "/agents",
        Some(agent_body("Finance Ops")),
    )
    .await?;
    assert_eq!(status, StatusCode::CREATED);
    assert_eq!(created["slug"], "finance-ops");
    let id = created["id"]
        .as_str()
        .ok_or_else(|| anyhow::anyhow!("missing id"))?;

    let (status, list) = request_json(&app.router, "GET", "/agents", None).await?;
    assert_eq!(status, StatusCode::OK);
    assert_eq!(
        list.as_array()
            .ok_or_else(|| anyhow::anyhow!("list not array"))?
            .len(),
        1
    );

    let detail_path = format!("/agents/{id}");
    let (status, detail) = request_json(&app.router, "GET", &detail_path, None).await?;
    assert_eq!(status, StatusCode::OK);
    assert_eq!(detail["name"], "Finance Ops");

    let (status, updated) = request_json(
        &app.router,
        "PATCH",
        &detail_path,
        Some(agent_body("Renamed")),
    )
    .await?;
    assert_eq!(status, StatusCode::OK);
    assert_eq!(updated["slug"], "renamed");

    let regen_path = format!("/agents/{id}/mcp-url:regenerate");
    let (status, regen) = request_json(&app.router, "POST", &regen_path, None).await?;
    assert_eq!(status, StatusCode::OK);
    assert_eq!(regen["id"], id);

    let (status, deleted) = request_json(&app.router, "DELETE", &detail_path, None).await?;
    assert_eq!(status, StatusCode::OK);
    assert_eq!(deleted["id"], id);
    Ok(())
}

#[tokio::test]
async fn site_rules_roundtrip() -> anyhow::Result<()> {
    let app = test_app().await?;
    let (status, empty) = request_json(&app.router, "GET", "/site-rules", None).await?;
    assert_eq!(status, StatusCode::OK);
    assert_eq!(
        empty
            .as_array()
            .ok_or_else(|| anyhow::anyhow!("site rules not array"))?
            .len(),
        0
    );
    let (status, created) = request_json(
        &app.router,
        "POST",
        "/site-rules",
        Some(json!({ "label": "Wire transfers", "domain": "mercury.com", "action": "payments" })),
    )
    .await?;
    assert_eq!(status, StatusCode::CREATED);
    let id = created["id"]
        .as_str()
        .ok_or_else(|| anyhow::anyhow!("missing id"))?;
    let delete_path = format!("/site-rules/{id}");
    let (status, deleted) = request_json(&app.router, "DELETE", &delete_path, None).await?;
    assert_eq!(status, StatusCode::OK);
    assert_eq!(deleted["id"], id);
    Ok(())
}

#[tokio::test]
async fn audit_empty_and_replay_gone() -> anyhow::Result<()> {
    let app = test_app().await?;
    let (status, dispatches) = request_json(&app.router, "GET", "/audit/dispatches", None).await?;
    assert_eq!(status, StatusCode::OK);
    assert_eq!(
        dispatches["rows"]
            .as_array()
            .ok_or_else(|| anyhow::anyhow!("rows not array"))?
            .len(),
        0
    );
    assert!(dispatches["nextCursor"].is_null());

    let (status, body) = request_json(
        &app.router,
        "POST",
        "/audit/replay/missing/events",
        Some(json!({ "type": 3 })),
    )
    .await?;
    assert_eq!(status, StatusCode::GONE);
    assert_eq!(body["error"], "session not live");
    Ok(())
}
