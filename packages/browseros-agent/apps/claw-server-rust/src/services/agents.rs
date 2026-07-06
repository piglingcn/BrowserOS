use crate::{
    error::{AppError, AppResult},
    services::{harness::HarnessService, now_iso, slugify},
    storage::JsonStore,
};
use serde::{Deserialize, Serialize};
use std::{collections::BTreeMap, fmt, str::FromStr, sync::Arc};
use tokio::sync::Mutex;
use ulid::Ulid;

const AGENTS_SUBDIR: &str = "agents";
const TOTAL_PROFILE_LOGINS: usize = 47;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum Harness {
    #[serde(rename = "Claude Code")]
    ClaudeCode,
    #[serde(rename = "Claude Desktop")]
    ClaudeDesktop,
    #[serde(rename = "Cursor")]
    Cursor,
    #[serde(rename = "VS Code")]
    VsCode,
    #[serde(rename = "Zed")]
    Zed,
    #[serde(rename = "Codex")]
    Codex,
    #[serde(rename = "Gemini CLI")]
    GeminiCli,
    #[serde(rename = "Hermes")]
    Hermes,
    #[serde(rename = "OpenClaw")]
    OpenClaw,
}

impl Harness {
    pub const ALL: [Harness; 9] = [
        Harness::ClaudeCode,
        Harness::ClaudeDesktop,
        Harness::Cursor,
        Harness::VsCode,
        Harness::Zed,
        Harness::Codex,
        Harness::GeminiCli,
        Harness::Hermes,
        Harness::OpenClaw,
    ];

    #[must_use]
    pub fn as_str(self) -> &'static str {
        match self {
            Self::ClaudeCode => "Claude Code",
            Self::ClaudeDesktop => "Claude Desktop",
            Self::Cursor => "Cursor",
            Self::VsCode => "VS Code",
            Self::Zed => "Zed",
            Self::Codex => "Codex",
            Self::GeminiCli => "Gemini CLI",
            Self::Hermes => "Hermes",
            Self::OpenClaw => "OpenClaw",
        }
    }

    #[must_use]
    pub fn agent_id(self) -> Option<&'static str> {
        match self {
            Self::ClaudeCode => Some("claude-code"),
            Self::ClaudeDesktop => Some("claude-desktop"),
            Self::Cursor => Some("cursor"),
            Self::VsCode => Some("vscode"),
            Self::Zed => Some("zed"),
            Self::Codex => Some("codex"),
            Self::GeminiCli => Some("gemini"),
            Self::Hermes | Self::OpenClaw => None,
        }
    }
}

impl fmt::Display for Harness {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

impl FromStr for Harness {
    type Err = AppError;

    fn from_str(value: &str) -> Result<Self, Self::Err> {
        let decoded = value.replace("%20", " ");
        match decoded.as_str() {
            "Claude Code" => Ok(Self::ClaudeCode),
            "Claude Desktop" => Ok(Self::ClaudeDesktop),
            "Cursor" => Ok(Self::Cursor),
            "VS Code" => Ok(Self::VsCode),
            "Zed" => Ok(Self::Zed),
            "Codex" => Ok(Self::Codex),
            "Gemini CLI" => Ok(Self::GeminiCli),
            "Hermes" => Ok(Self::Hermes),
            "OpenClaw" => Ok(Self::OpenClaw),
            _ => Err(AppError::bad_request("unsupported harness")),
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum LoginMode {
    Profile,
    All,
    Selective,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum ApprovalVerdict {
    Auto,
    Ask,
    Block,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum ProfileStatus {
    Configured,
    Paused,
    Disabled,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct CustomAclRule {
    pub id: String,
    pub label: String,
    pub domain: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct NewAgentValues {
    pub name: String,
    pub harness: Harness,
    pub login_mode: LoginMode,
    pub selected_sites: Vec<String>,
    pub approvals: BTreeMap<String, ApprovalVerdict>,
    pub acl_rule_ids: Vec<String>,
    pub custom_acl_rules: Vec<CustomAclRule>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct StoredAgentProfile {
    pub name: String,
    pub harness: Harness,
    pub login_mode: LoginMode,
    pub selected_sites: Vec<String>,
    pub approvals: BTreeMap<String, ApprovalVerdict>,
    pub acl_rule_ids: Vec<String>,
    pub custom_acl_rules: Vec<CustomAclRule>,
    pub id: String,
    pub slug: String,
    pub mcp_url: String,
    pub status: ProfileStatus,
    pub created_at: String,
    pub updated_at: String,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct AgentProfileSummary {
    pub id: String,
    pub name: String,
    pub harness: Harness,
    pub login_scope_label: String,
    pub login_count: usize,
    pub acl_rule_count: usize,
    pub blocked_action_count: usize,
    pub always_allow_count: usize,
    pub last_run_at: String,
    pub status: ProfileStatus,
    pub mcp_url: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct HarnessInstallOutcome {
    pub installed: bool,
    pub message: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub agent: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub config_path: Option<String>,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct CreatedAgent {
    pub id: String,
    pub name: String,
    pub harness: Harness,
    pub slug: String,
    pub mcp_url: String,
    pub cli_command: String,
    pub harness_install: HarnessInstallOutcome,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct DeletedAgent {
    pub id: String,
    pub harness_uninstall: HarnessInstallOutcome,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct RegeneratedMcpUrl {
    pub id: String,
    pub mcp_url: String,
}

#[derive(Clone)]
pub struct AgentService {
    store: JsonStore,
    harness: Arc<HarnessService>,
    public_mcp_url: String,
    slug_mutex: Arc<Mutex<()>>,
}

impl AgentService {
    #[must_use]
    pub fn new(store: JsonStore, harness: Arc<HarnessService>, public_mcp_url: String) -> Self {
        Self {
            store,
            harness,
            public_mcp_url,
            slug_mutex: Arc::new(Mutex::new(())),
        }
    }

    pub async fn list_profiles(&self) -> AppResult<Vec<StoredAgentProfile>> {
        let names = self.store.list_files(AGENTS_SUBDIR, ".json").await?;
        let mut out = Vec::new();
        for name in names {
            let rel = format!("{AGENTS_SUBDIR}/{name}");
            match self.store.read_json::<StoredAgentProfile>(&rel).await {
                Ok(profile) => out.push(profile),
                Err(err) => {
                    tracing::warn!(file = %rel, error = %err, "skipping unreadable agent profile")
                }
            }
        }
        Ok(out)
    }

    pub async fn list(&self) -> AppResult<Vec<AgentProfileSummary>> {
        let mut profiles = self.list_profiles().await?;
        profiles.sort_by(|a, b| b.updated_at.cmp(&a.updated_at));
        Ok(profiles
            .iter()
            .map(|profile| self.summarize(profile))
            .collect())
    }

    pub async fn get_detail(&self, id: &str) -> AppResult<Option<NewAgentValues>> {
        Ok(self
            .load_by_id(id)
            .await?
            .map(|profile| profile.into_new_values()))
    }

    pub async fn load_by_id(&self, id: &str) -> AppResult<Option<StoredAgentProfile>> {
        if !is_valid_id(id) {
            return Ok(None);
        }
        let rel = file_for(id);
        match self.store.read_json(&rel).await {
            Ok(profile) => Ok(Some(profile)),
            Err(AppError::StorageNotFound(_)) | Err(AppError::InvalidStoragePath(_)) => Ok(None),
            Err(err) => Err(err),
        }
    }

    pub async fn create(&self, input: NewAgentValues) -> AppResult<CreatedAgent> {
        input.validate()?;
        let _guard = self.slug_mutex.lock().await;
        let id = Ulid::new().to_string();
        let existing = self.list_profiles().await?;
        let slug = unique_slug(
            &slugify(&input.name),
            existing.iter().map(|p| p.slug.as_str()),
        );
        let now = now_iso();
        let profile = StoredAgentProfile {
            name: input.name,
            harness: input.harness,
            login_mode: input.login_mode,
            selected_sites: input.selected_sites,
            approvals: input.approvals,
            acl_rule_ids: input.acl_rule_ids,
            custom_acl_rules: input.custom_acl_rules,
            id: id.clone(),
            slug: slug.clone(),
            mcp_url: self.public_mcp_url.clone(),
            status: ProfileStatus::Configured,
            created_at: now.clone(),
            updated_at: now,
        };
        self.store.write_json(&file_for(&id), &profile).await?;
        let harness_install = self.harness.install_for_agent(&profile).await;
        Ok(CreatedAgent {
            id,
            name: profile.name,
            harness: profile.harness,
            slug: slug.clone(),
            mcp_url: profile.mcp_url,
            cli_command: format!("mcp add {slug}"),
            harness_install,
        })
    }

    pub async fn update(
        &self,
        id: &str,
        input: NewAgentValues,
    ) -> AppResult<Option<StoredAgentProfile>> {
        input.validate()?;
        let _guard = self.slug_mutex.lock().await;
        let Some(existing) = self.load_by_id(id).await? else {
            return Ok(None);
        };
        let profiles = self.list_profiles().await?;
        let name_slug = slugify(&input.name);
        let slug = if name_slug == slugify(&existing.name) {
            existing.slug.clone()
        } else {
            unique_slug(
                &name_slug,
                profiles
                    .iter()
                    .filter(|profile| profile.id != id)
                    .map(|profile| profile.slug.as_str()),
            )
        };
        let next = StoredAgentProfile {
            name: input.name,
            harness: input.harness,
            login_mode: input.login_mode,
            selected_sites: input.selected_sites,
            approvals: input.approvals,
            acl_rule_ids: input.acl_rule_ids,
            custom_acl_rules: input.custom_acl_rules,
            id: id.to_string(),
            slug,
            mcp_url: self.public_mcp_url.clone(),
            status: existing.status,
            created_at: existing.created_at.clone(),
            updated_at: now_iso(),
        };
        self.store.write_json(&file_for(id), &next).await?;
        self.harness.reconcile_agent_link(&existing, &next).await;
        Ok(Some(next))
    }

    pub async fn remove(&self, id: &str) -> AppResult<Option<DeletedAgent>> {
        if !is_valid_id(id) {
            return Ok(None);
        }
        let Some(profile) = self.load_by_id(id).await? else {
            return Ok(None);
        };
        if !self.store.remove_file(&file_for(id)).await? {
            return Ok(None);
        }
        let harness_uninstall = self.harness.uninstall_for_agent(&profile).await;
        Ok(Some(DeletedAgent {
            id: id.to_string(),
            harness_uninstall,
        }))
    }

    pub async fn regenerate_mcp_url(&self, id: &str) -> AppResult<Option<RegeneratedMcpUrl>> {
        let _guard = self.slug_mutex.lock().await;
        let Some(existing) = self.load_by_id(id).await? else {
            return Ok(None);
        };
        let profiles = self.list_profiles().await?;
        let base = slugify(&format!("{} {}", existing.name, Ulid::new()));
        let slug = unique_slug(
            &base,
            profiles
                .iter()
                .filter(|profile| profile.id != id)
                .map(|profile| profile.slug.as_str()),
        );
        let next = StoredAgentProfile {
            slug,
            mcp_url: self.public_mcp_url.clone(),
            updated_at: now_iso(),
            ..existing.clone()
        };
        self.store.write_json(&file_for(id), &next).await?;
        self.harness.reconcile_agent_link(&existing, &next).await;
        Ok(Some(RegeneratedMcpUrl {
            id: id.to_string(),
            mcp_url: next.mcp_url,
        }))
    }

    fn summarize(&self, profile: &StoredAgentProfile) -> AgentProfileSummary {
        let blocked_action_count = profile
            .approvals
            .values()
            .filter(|value| matches!(value, ApprovalVerdict::Block))
            .count();
        let login_count = match profile.login_mode {
            LoginMode::Selective => profile.selected_sites.len(),
            LoginMode::Profile | LoginMode::All => TOTAL_PROFILE_LOGINS,
        };
        let login_scope_label = match profile.login_mode {
            LoginMode::Selective => format!("Selective ({})", profile.selected_sites.len()),
            LoginMode::All => format!("All my logins ({TOTAL_PROFILE_LOGINS})"),
            LoginMode::Profile => format!("Current profile ({TOTAL_PROFILE_LOGINS})"),
        };
        AgentProfileSummary {
            id: profile.id.clone(),
            name: profile.name.clone(),
            harness: profile.harness,
            login_scope_label,
            login_count,
            acl_rule_count: profile.acl_rule_ids.len(),
            blocked_action_count,
            always_allow_count: 0,
            last_run_at: "Never run".to_string(),
            status: profile.status,
            mcp_url: self.public_mcp_url.clone(),
        }
    }
}

impl NewAgentValues {
    pub fn validate(&self) -> AppResult<()> {
        if self.name.trim().is_empty() {
            return Err(AppError::bad_request("name is required"));
        }
        for rule in &self.custom_acl_rules {
            if rule.label.trim().is_empty() || rule.domain.trim().is_empty() {
                return Err(AppError::bad_request(
                    "custom ACL rules require label and domain",
                ));
            }
        }
        Ok(())
    }
}

impl StoredAgentProfile {
    #[must_use]
    pub fn into_new_values(self) -> NewAgentValues {
        NewAgentValues {
            name: self.name,
            harness: self.harness,
            login_mode: self.login_mode,
            selected_sites: self.selected_sites,
            approvals: self.approvals,
            acl_rule_ids: self.acl_rule_ids,
            custom_acl_rules: self.custom_acl_rules,
        }
    }
}

fn file_for(id: &str) -> String {
    format!("{AGENTS_SUBDIR}/{id}.json")
}

fn is_valid_id(id: &str) -> bool {
    !id.is_empty()
        && id.len() <= 64
        && id
            .chars()
            .all(|ch| ch.is_ascii_alphanumeric() || ch == '_' || ch == '-')
}

fn unique_slug<'a>(base: &str, existing: impl Iterator<Item = &'a str>) -> String {
    let existing: std::collections::BTreeSet<&str> = existing.collect();
    if !existing.contains(base) {
        return base.to_string();
    }
    for suffix in 2..=99 {
        let candidate = format!("{base}-{suffix}");
        if !existing.contains(candidate.as_str()) {
            return candidate;
        }
    }
    format!("{base}-{}", Ulid::new())
}
