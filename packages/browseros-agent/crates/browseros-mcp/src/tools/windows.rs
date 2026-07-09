use crate::framework::{
    ToolCtx, ToolExecResult, ToolResult, error_result, parse_args, text_result,
};
use browseros_core::WindowId;
use futures_util::future::BoxFuture;
use schemars::JsonSchema;
use serde::Deserialize;
use serde_json::{Value, json};

const DESCRIPTION: &str = "\
Manage browser windows: list windows, create visible or hidden windows, \
close or activate a window, and show or hide windows.";

#[derive(Debug, Clone, Default, Deserialize, JsonSchema)]
#[serde(rename_all = "snake_case")]
enum WindowsAction {
    #[default]
    List,
    Create,
    Close,
    Activate,
    SetVisibility,
}

#[derive(Debug, Clone, Deserialize, JsonSchema)]
struct WindowsArgs {
    #[serde(default)]
    action: WindowsAction,
    /// Window id for close, activate, and set_visibility.
    #[serde(rename = "windowId")]
    window_id: Option<i64>,
    /// Create a hidden window for action="create".
    #[serde(default)]
    hidden: bool,
    /// Target visibility for action="set_visibility".
    visible: Option<bool>,
    /// Focus the window after making it visible.
    activate: Option<bool>,
}

pub fn definition() -> crate::framework::ToolDef {
    super::def::<WindowsArgs>(
        "windows",
        DESCRIPTION,
        Some(super::open_world_annotations()),
        handler,
    )
}

fn handler<'a>(
    raw: Value,
    ctx: &'a ToolCtx,
    _response: &'a mut crate::response::ToolResponse,
) -> BoxFuture<'a, ToolExecResult<Option<ToolResult>>> {
    Box::pin(async move {
        let args: WindowsArgs = parse_args(raw)?;
        let result = match args.action {
            WindowsAction::List => {
                let windows = ctx.session.windows.list().await?;
                text_result(
                    format_window_list(&windows),
                    Some(json!({ "action": "list", "windows": windows, "count": windows.len() })),
                )
            }
            WindowsAction::Create => {
                let window = ctx.session.windows.create(args.hidden).await?;
                let hidden_marker = if !window.is_visible { " (hidden)" } else { "" };
                text_result(
                    format!("created window {}{hidden_marker}", window.window_id),
                    Some(json!({ "action": "create", "window": window })),
                )
            }
            WindowsAction::Close => {
                let Some(window_id) = args.window_id else {
                    return Ok(Some(error_result("windows close: windowId is required.")));
                };
                ctx.session.windows.close(WindowId(window_id)).await?;
                text_result(
                    format!("closed window {window_id}"),
                    Some(json!({ "action": "close", "windowId": window_id })),
                )
            }
            WindowsAction::Activate => {
                let Some(window_id) = args.window_id else {
                    return Ok(Some(error_result(
                        "windows activate: windowId is required.",
                    )));
                };
                ctx.session.windows.activate(WindowId(window_id)).await?;
                text_result(
                    format!("activated window {window_id}"),
                    Some(json!({ "action": "activate", "windowId": window_id })),
                )
            }
            WindowsAction::SetVisibility => {
                let Some(window_id) = args.window_id else {
                    return Ok(Some(error_result(
                        "windows set_visibility: windowId is required.",
                    )));
                };
                let Some(visible) = args.visible else {
                    return Ok(Some(error_result(
                        "windows set_visibility: visible is required.",
                    )));
                };
                let result = ctx
                    .session
                    .windows
                    .set_visibility(WindowId(window_id), visible, args.activate)
                    .await?;
                let state = if result.window.is_visible {
                    "visible"
                } else {
                    "hidden"
                };
                text_result(
                    format!(
                        "set window {} {state}; new window id {}",
                        result.previous_window_id.0, result.new_window_id.0
                    ),
                    Some(json!({
                        "action": "set_visibility",
                        "previousWindowId": result.previous_window_id.0,
                        "newWindowId": result.new_window_id.0,
                        "replaced": result.replaced,
                        "window": result.window
                    })),
                )
            }
        };
        Ok(Some(result))
    })
}

fn format_window_list(windows: &[browseros_core::windows::WindowInfo]) -> String {
    if windows.is_empty() {
        return "No windows found.".to_string();
    }
    let mut lines = vec![format!("Found {} windows:", windows.len()), String::new()];
    for window in windows {
        let mut markers = Vec::new();
        if !window.is_visible {
            markers.push("HIDDEN");
        }
        if window.is_active {
            markers.push("ACTIVE");
        }
        let suffix = if markers.is_empty() {
            String::new()
        } else {
            format!(" [{}]", markers.join(", "))
        };
        lines.push(format!(
            "Window {} ({}, {} tabs){suffix}",
            window.window_id, window.window_type, window.tab_count
        ));
    }
    lines.join("\n")
}
