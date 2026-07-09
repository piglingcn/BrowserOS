use crate::{
    error::AppResult,
    services::{agents::Harness, harness::surfaces},
};
use std::path::PathBuf;
use surfaces::McpServerSpec;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PlanAction {
    Connect,
    Disconnect,
}

#[derive(Debug, Clone)]
pub struct PlanOutcome {
    pub action: PlanAction,
    pub harness: Harness,
    pub server_name: String,
    pub spec: McpServerSpec,
    pub config_path: PathBuf,
}

pub async fn build_plan(
    action: PlanAction,
    harness: Harness,
    server_name: &str,
    spec: McpServerSpec,
    config_path: PathBuf,
) -> AppResult<PlanOutcome> {
    Ok(PlanOutcome {
        action,
        harness,
        server_name: server_name.to_string(),
        spec,
        config_path,
    })
}

impl PlanOutcome {
    pub async fn verify(&self) -> AppResult<bool> {
        surfaces::has_entry(self.harness, &self.config_path, &self.server_name).await
    }

    pub async fn apply(&self, allow_overwrite: bool) -> AppResult<()> {
        match self.action {
            PlanAction::Connect => {
                let before = tokio::fs::read(&self.config_path).await.ok();
                let result = surfaces::write_entry(
                    self.harness,
                    &self.config_path,
                    &self.server_name,
                    &self.spec,
                    allow_overwrite,
                )
                .await;
                if let Err(err) = result {
                    self.rollback(before).await?;
                    return Err(err);
                }
            }
            PlanAction::Disconnect => {
                surfaces::remove_entry(self.harness, &self.config_path, &self.server_name).await?;
            }
        }
        Ok(())
    }

    pub async fn rollback(&self, before: Option<Vec<u8>>) -> AppResult<()> {
        match before {
            Some(bytes) => {
                if let Some(parent) = self.config_path.parent() {
                    tokio::fs::create_dir_all(parent).await?;
                }
                tokio::fs::write(&self.config_path, bytes).await?;
            }
            None => {
                let _ = tokio::fs::remove_file(&self.config_path).await;
            }
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::services::{
        agents::Harness,
        harness::{HarnessService, surfaces::SurfacePaths},
    };
    use tempfile::tempdir;

    #[tokio::test]
    async fn dry_run_and_apply_for_each_surface() -> anyhow::Result<()> {
        let dir = tempdir()?;
        let paths = SurfacePaths {
            claude_code: Some(dir.path().join("claude.json")),
            claude_desktop: Some(dir.path().join("claude_desktop.json")),
            cursor: Some(dir.path().join("cursor.json")),
            vscode: Some(dir.path().join("vscode.json")),
            zed: Some(dir.path().join("zed.json")),
            codex: Some(dir.path().join("config.toml")),
            gemini: Some(dir.path().join("gemini.json")),
        };
        let service = HarnessService::with_paths(
            dir.path().join("mcp-manager"),
            dir.path().to_path_buf(),
            paths,
        );
        for harness in [
            Harness::ClaudeCode,
            Harness::ClaudeDesktop,
            Harness::Cursor,
            Harness::VsCode,
            Harness::Zed,
            Harness::Codex,
            Harness::GeminiCli,
        ] {
            let plan = service
                .dry_run(harness, "BrowserClaw", "http://127.0.0.1:9200/mcp")
                .await?;
            assert!(!plan.verify().await?);
            plan.apply(true).await?;
            assert!(plan.verify().await?);
        }
        Ok(())
    }
}
