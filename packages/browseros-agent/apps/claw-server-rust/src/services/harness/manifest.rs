use crate::{
    error::{AppError, AppResult, IoPath},
    services::harness::surfaces::McpServerSpec,
};
use serde::{Deserialize, Serialize};
use std::{collections::BTreeMap, path::Path};
use tokio::fs;

const MANIFEST_FILE: &str = "manifest.json";

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct HarnessManifest {
    pub version: u32,
    pub servers: BTreeMap<String, HarnessServer>,
    pub links: Vec<HarnessLink>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct HarnessServer {
    pub spec: McpServerSpec,
    pub added_at: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct HarnessLink {
    pub server_name: String,
    pub agent: String,
    pub config_path: String,
}

impl Default for HarnessManifest {
    fn default() -> Self {
        Self {
            version: 1,
            servers: BTreeMap::new(),
            links: Vec::new(),
        }
    }
}

impl HarnessManifest {
    pub async fn load(workspace_dir: &Path) -> AppResult<Self> {
        let path = workspace_dir.join(MANIFEST_FILE);
        match fs::read_to_string(&path).await {
            Ok(raw) => serde_json::from_str(&raw).map_err(AppError::from),
            Err(err) if err.kind() == std::io::ErrorKind::NotFound => Ok(Self::default()),
            Err(source) => Err(AppError::Io {
                path: Some(path),
                source,
            }),
        }
    }

    pub async fn save(&self, workspace_dir: &Path) -> AppResult<()> {
        fs::create_dir_all(workspace_dir)
            .await
            .with_path(workspace_dir)?;
        let path = workspace_dir.join(MANIFEST_FILE);
        let tmp = workspace_dir.join("manifest.json.tmp");
        fs::write(&tmp, serde_json::to_string_pretty(self)?)
            .await
            .with_path(tmp.clone())?;
        fs::rename(&tmp, &path).await.with_path(path)
    }

    pub fn upsert_server(&mut self, name: &str, spec: McpServerSpec) {
        self.servers.insert(
            name.to_string(),
            HarnessServer {
                spec,
                added_at: crate::services::now_iso(),
            },
        );
    }

    pub fn remove_server(&mut self, name: &str) {
        self.servers.remove(name);
    }

    pub fn upsert_link(&mut self, link: HarnessLink) {
        self.links.retain(|existing| {
            !(existing.server_name == link.server_name && existing.agent == link.agent)
        });
        self.links.push(link);
    }

    pub fn remove_link(&mut self, server_name: &str, agent: &str) {
        self.links
            .retain(|link| !(link.server_name == server_name && link.agent == agent));
    }

    #[must_use]
    pub fn has_links(&self, server_name: &str) -> bool {
        self.links
            .iter()
            .any(|link| link.server_name == server_name)
    }
}
