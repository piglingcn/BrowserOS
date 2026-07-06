use crate::{
    error::{AppError, AppResult, IoPath},
    services::agents::Harness,
};
use serde::{Deserialize, Serialize};
use serde_json::{Map, Value, json};
use std::{
    collections::BTreeMap,
    path::{Path, PathBuf},
};
use tokio::fs;

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(tag = "transport", rename_all = "lowercase")]
pub enum McpServerSpec {
    Http {
        url: String,
        #[serde(default, skip_serializing_if = "BTreeMap::is_empty")]
        headers: BTreeMap<String, String>,
    },
    Stdio {
        command: String,
        #[serde(default)]
        args: Vec<String>,
        #[serde(default, skip_serializing_if = "BTreeMap::is_empty")]
        env: BTreeMap<String, String>,
    },
}

#[derive(Debug, Clone, Default)]
pub struct SurfacePaths {
    pub claude_code: Option<PathBuf>,
    pub claude_desktop: Option<PathBuf>,
    pub cursor: Option<PathBuf>,
    pub vscode: Option<PathBuf>,
    pub zed: Option<PathBuf>,
    pub codex: Option<PathBuf>,
    pub gemini: Option<PathBuf>,
}

pub fn config_path_for(harness: Harness, home: &Path, paths: &SurfacePaths) -> AppResult<PathBuf> {
    let path = match harness {
        Harness::ClaudeCode => paths
            .claude_code
            .clone()
            .unwrap_or_else(|| home.join(".claude.json")),
        Harness::ClaudeDesktop => paths.claude_desktop.clone().unwrap_or_else(|| {
            home.join("Library")
                .join("Application Support")
                .join("Claude")
                .join("claude_desktop_config.json")
        }),
        Harness::Cursor => paths
            .cursor
            .clone()
            .unwrap_or_else(|| home.join(".cursor").join("mcp.json")),
        Harness::VsCode => paths.vscode.clone().unwrap_or_else(|| {
            home.join("Library")
                .join("Application Support")
                .join("Code")
                .join("User")
                .join("mcp.json")
        }),
        Harness::Zed => paths
            .zed
            .clone()
            .unwrap_or_else(|| home.join(".config").join("zed").join("settings.json")),
        Harness::Codex => paths
            .codex
            .clone()
            .unwrap_or_else(|| home.join(".codex").join("config.toml")),
        Harness::GeminiCli => paths
            .gemini
            .clone()
            .unwrap_or_else(|| home.join(".gemini").join("settings.json")),
        Harness::Hermes | Harness::OpenClaw => {
            return Err(AppError::bad_request("internal harness has no config path"));
        }
    };
    Ok(path)
}

pub async fn write_entry(
    harness: Harness,
    path: &Path,
    server_name: &str,
    spec: &McpServerSpec,
    allow_overwrite: bool,
) -> AppResult<()> {
    match harness {
        Harness::Codex => write_codex_entry(path, server_name, spec, allow_overwrite).await,
        Harness::VsCode => {
            write_json_entry(
                path,
                "servers",
                server_name,
                spec,
                allow_overwrite,
                vs_code_entry,
            )
            .await
        }
        Harness::Zed => {
            write_json_entry(
                path,
                "context_servers",
                server_name,
                spec,
                allow_overwrite,
                zed_entry,
            )
            .await
        }
        Harness::ClaudeCode => {
            write_json_entry(
                path,
                "mcpServers",
                server_name,
                spec,
                allow_overwrite,
                claude_code_entry,
            )
            .await
        }
        Harness::ClaudeDesktop | Harness::Cursor | Harness::GeminiCli => {
            write_json_entry(
                path,
                "mcpServers",
                server_name,
                spec,
                allow_overwrite,
                generic_entry,
            )
            .await
        }
        Harness::Hermes | Harness::OpenClaw => Ok(()),
    }
}

pub async fn remove_entry(harness: Harness, path: &Path, server_name: &str) -> AppResult<()> {
    match harness {
        Harness::Codex => remove_codex_entry(path, server_name).await,
        Harness::VsCode => remove_json_entry(path, "servers", server_name).await,
        Harness::Zed => remove_json_entry(path, "context_servers", server_name).await,
        Harness::ClaudeCode | Harness::ClaudeDesktop | Harness::Cursor | Harness::GeminiCli => {
            remove_json_entry(path, "mcpServers", server_name).await
        }
        Harness::Hermes | Harness::OpenClaw => Ok(()),
    }
}

pub async fn has_entry(harness: Harness, path: &Path, server_name: &str) -> AppResult<bool> {
    match harness {
        Harness::Codex => {
            let value = read_toml_value(path).await?;
            Ok(value
                .get("mcp_servers")
                .and_then(toml::Value::as_table)
                .and_then(|table| table.get(server_name))
                .is_some())
        }
        Harness::VsCode => json_has_entry(path, "servers", server_name).await,
        Harness::Zed => json_has_entry(path, "context_servers", server_name).await,
        Harness::ClaudeCode | Harness::ClaudeDesktop | Harness::Cursor | Harness::GeminiCli => {
            json_has_entry(path, "mcpServers", server_name).await
        }
        Harness::Hermes | Harness::OpenClaw => Ok(true),
    }
}

pub async fn heal_claude_code_http_tags(path: &Path) -> AppResult<usize> {
    let mut root = read_json_value(path).await?;
    let Some(servers) = root.get_mut("mcpServers").and_then(Value::as_object_mut) else {
        return Ok(0);
    };
    let mut changed = 0;
    for name in ["BrowserClaw", "browseros"] {
        let Some(entry) = servers.get_mut(name).and_then(Value::as_object_mut) else {
            continue;
        };
        let is_local = entry
            .get("url")
            .and_then(Value::as_str)
            .map(is_local_mcp_url)
            .unwrap_or(false);
        if is_local && !entry.contains_key("type") {
            entry.insert("type".to_string(), Value::String("http".to_string()));
            changed += 1;
        }
    }
    if changed > 0 {
        write_json_value(path, &root).await?;
    }
    Ok(changed)
}

fn generic_entry(spec: &McpServerSpec) -> Value {
    match spec {
        McpServerSpec::Http { url, .. } => json!({ "url": url }),
        McpServerSpec::Stdio { command, args, env } => {
            let mut value = json!({ "command": command, "args": args });
            if !env.is_empty()
                && let Some(obj) = value.as_object_mut()
            {
                obj.insert("env".to_string(), json!(env));
            }
            value
        }
    }
}

fn claude_code_entry(spec: &McpServerSpec) -> Value {
    match spec {
        McpServerSpec::Http { url, .. } => json!({ "type": "http", "url": url }),
        McpServerSpec::Stdio { command, args, env } => {
            let mut value = json!({ "type": "stdio", "command": command, "args": args });
            if !env.is_empty()
                && let Some(obj) = value.as_object_mut()
            {
                obj.insert("env".to_string(), json!(env));
            }
            value
        }
    }
}

fn vs_code_entry(spec: &McpServerSpec) -> Value {
    let mut value = generic_entry(spec);
    if let Some(obj) = value.as_object_mut() {
        obj.insert(
            "type".to_string(),
            Value::String(
                match spec {
                    McpServerSpec::Http { .. } => "http",
                    McpServerSpec::Stdio { .. } => "stdio",
                }
                .to_string(),
            ),
        );
    }
    value
}

fn zed_entry(spec: &McpServerSpec) -> Value {
    let mut value = generic_entry(spec);
    if let Some(obj) = value.as_object_mut() {
        obj.insert("source".to_string(), Value::String("custom".to_string()));
        obj.insert("enabled".to_string(), Value::Bool(true));
    }
    value
}

async fn write_json_entry(
    path: &Path,
    key: &str,
    server_name: &str,
    spec: &McpServerSpec,
    _allow_overwrite: bool,
    render: fn(&McpServerSpec) -> Value,
) -> AppResult<()> {
    let mut root = read_json_value(path).await?;
    let object = ensure_object(&mut root);
    let servers = object
        .entry(key.to_string())
        .or_insert_with(|| Value::Object(Map::new()));
    let Some(server_map) = servers.as_object_mut() else {
        return Err(AppError::bad_request(format!("{key} must be an object")));
    };
    server_map.insert(server_name.to_string(), render(spec));
    write_json_value(path, &root).await
}

async fn remove_json_entry(path: &Path, key: &str, server_name: &str) -> AppResult<()> {
    let mut root = read_json_value(path).await?;
    if let Some(map) = root.get_mut(key).and_then(Value::as_object_mut) {
        map.remove(server_name);
    }
    write_json_value(path, &root).await
}

async fn json_has_entry(path: &Path, key: &str, server_name: &str) -> AppResult<bool> {
    let root = read_json_value(path).await?;
    Ok(root
        .get(key)
        .and_then(Value::as_object)
        .map(|map| map.contains_key(server_name))
        .unwrap_or(false))
}

async fn write_codex_entry(
    path: &Path,
    server_name: &str,
    spec: &McpServerSpec,
    _allow_overwrite: bool,
) -> AppResult<()> {
    let mut root = read_toml_value(path).await?;
    let table = root
        .as_table_mut()
        .ok_or_else(|| AppError::bad_request("Codex config root must be a TOML table"))?;
    let servers = table
        .entry("mcp_servers".to_string())
        .or_insert_with(|| toml::Value::Table(toml::map::Map::new()));
    let Some(server_table) = servers.as_table_mut() else {
        return Err(AppError::bad_request("mcp_servers must be a TOML table"));
    };
    server_table.insert(server_name.to_string(), spec_to_toml(spec));
    write_toml_value(path, &root).await
}

async fn remove_codex_entry(path: &Path, server_name: &str) -> AppResult<()> {
    let mut root = read_toml_value(path).await?;
    if let Some(map) = root
        .get_mut("mcp_servers")
        .and_then(toml::Value::as_table_mut)
    {
        map.remove(server_name);
    }
    write_toml_value(path, &root).await
}

fn spec_to_toml(spec: &McpServerSpec) -> toml::Value {
    let mut table = toml::map::Map::new();
    match spec {
        McpServerSpec::Http { url, headers } => {
            table.insert("url".to_string(), toml::Value::String(url.clone()));
            if !headers.is_empty() {
                table.insert(
                    "http_headers".to_string(),
                    toml::Value::try_from(headers)
                        .unwrap_or_else(|_| toml::Value::Table(toml::map::Map::new())),
                );
            }
        }
        McpServerSpec::Stdio { command, args, env } => {
            table.insert("command".to_string(), toml::Value::String(command.clone()));
            table.insert(
                "args".to_string(),
                toml::Value::Array(args.iter().cloned().map(toml::Value::String).collect()),
            );
            if !env.is_empty() {
                table.insert(
                    "env".to_string(),
                    toml::Value::try_from(env)
                        .unwrap_or_else(|_| toml::Value::Table(toml::map::Map::new())),
                );
            }
        }
    }
    toml::Value::Table(table)
}

async fn read_json_value(path: &Path) -> AppResult<Value> {
    match fs::read_to_string(path).await {
        Ok(raw) if raw.trim().is_empty() => Ok(Value::Object(Map::new())),
        Ok(raw) => serde_json::from_str(&raw).map_err(AppError::from),
        Err(err) if err.kind() == std::io::ErrorKind::NotFound => Ok(Value::Object(Map::new())),
        Err(source) => Err(AppError::Io {
            path: Some(path.to_path_buf()),
            source,
        }),
    }
}

async fn write_json_value(path: &Path, value: &Value) -> AppResult<()> {
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent).await.with_path(parent)?;
    }
    let body = serde_json::to_string_pretty(value)?;
    fs::write(path, format!("{body}\n")).await.with_path(path)
}

async fn read_toml_value(path: &Path) -> AppResult<toml::Value> {
    match fs::read_to_string(path).await {
        Ok(raw) if raw.trim().is_empty() => Ok(toml::Value::Table(toml::map::Map::new())),
        Ok(raw) => raw.parse::<toml::Value>().map_err(|err| {
            AppError::Internal(format!("invalid TOML at {}: {err}", path.display()))
        }),
        Err(err) if err.kind() == std::io::ErrorKind::NotFound => {
            Ok(toml::Value::Table(toml::map::Map::new()))
        }
        Err(source) => Err(AppError::Io {
            path: Some(path.to_path_buf()),
            source,
        }),
    }
}

async fn write_toml_value(path: &Path, value: &toml::Value) -> AppResult<()> {
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent).await.with_path(parent)?;
    }
    let body = toml::to_string_pretty(value)
        .map_err(|err| AppError::Internal(format!("failed to encode TOML: {err}")))?;
    fs::write(path, body).await.with_path(path)
}

fn ensure_object(value: &mut Value) -> &mut Map<String, Value> {
    if !value.is_object() {
        *value = Value::Object(Map::new());
    }
    match value {
        Value::Object(map) => map,
        _ => unreachable!("value forced to object"),
    }
}

fn is_local_mcp_url(value: &str) -> bool {
    let Some(rest) = value.strip_prefix("http://127.0.0.1:") else {
        return false;
    };
    let Some(port) = rest.strip_suffix("/mcp") else {
        return false;
    };
    !port.is_empty() && port.chars().all(|ch| ch.is_ascii_digit())
}
