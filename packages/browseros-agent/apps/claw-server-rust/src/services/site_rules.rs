use crate::{
    error::{AppError, AppResult},
    storage::JsonStore,
};
use serde::{Deserialize, Serialize};
use std::{fmt, str::FromStr, sync::Arc};
use tokio::sync::Mutex;
use ulid::Ulid;

const FILE: &str = "site-rules.json";

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum SiteRuleAction {
    Payments,
    Submit,
    Delete,
    Navigate,
    Upload,
    Admin,
}

impl fmt::Display for SiteRuleAction {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(match self {
            Self::Payments => "payments",
            Self::Submit => "submit",
            Self::Delete => "delete",
            Self::Navigate => "navigate",
            Self::Upload => "upload",
            Self::Admin => "admin",
        })
    }
}

impl FromStr for SiteRuleAction {
    type Err = AppError;

    fn from_str(value: &str) -> Result<Self, Self::Err> {
        match value {
            "payments" => Ok(Self::Payments),
            "submit" => Ok(Self::Submit),
            "delete" => Ok(Self::Delete),
            "navigate" => Ok(Self::Navigate),
            "upload" => Ok(Self::Upload),
            "admin" => Ok(Self::Admin),
            _ => Err(AppError::bad_request("unsupported site-rule action")),
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct AddSiteRule {
    pub label: String,
    pub domain: String,
    pub action: SiteRuleAction,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct SiteRule {
    pub id: String,
    pub label: String,
    pub domain: String,
    pub action: SiteRuleAction,
}

#[derive(Clone)]
pub struct SiteRulesService {
    store: JsonStore,
    mutex: Arc<Mutex<()>>,
}

impl SiteRulesService {
    #[must_use]
    pub fn new(store: JsonStore) -> Self {
        Self {
            store,
            mutex: Arc::new(Mutex::new(())),
        }
    }

    pub async fn list(&self) -> AppResult<Vec<SiteRule>> {
        match self.store.read_json(FILE).await {
            Ok(rules) => Ok(rules),
            Err(AppError::StorageNotFound(_)) => Ok(Vec::new()),
            Err(err) => Err(err),
        }
    }

    pub async fn add(&self, input: AddSiteRule) -> AppResult<SiteRule> {
        input.validate()?;
        let _guard = self.mutex.lock().await;
        let mut rules = self.list().await?;
        let rule = SiteRule {
            id: Ulid::new().to_string(),
            label: input.label,
            domain: input.domain,
            action: input.action,
        };
        rules.push(rule.clone());
        self.store.write_json(FILE, &rules).await?;
        Ok(rule)
    }

    pub async fn remove(&self, id: &str) -> AppResult<Option<serde_json::Value>> {
        if !is_valid_id(id) {
            return Ok(None);
        }
        let _guard = self.mutex.lock().await;
        if !self.store.file_exists(FILE).await? {
            return Ok(None);
        }
        let rules = self.list().await?;
        let original_len = rules.len();
        let next: Vec<SiteRule> = rules.into_iter().filter(|rule| rule.id != id).collect();
        if next.len() == original_len {
            return Ok(None);
        }
        self.store.write_json(FILE, &next).await?;
        Ok(Some(serde_json::json!({ "id": id })))
    }
}

impl AddSiteRule {
    fn validate(&self) -> AppResult<()> {
        if self.label.trim().is_empty() {
            return Err(AppError::bad_request("label is required"));
        }
        if self.domain.trim().is_empty() {
            return Err(AppError::bad_request("domain is required"));
        }
        Ok(())
    }
}

fn is_valid_id(id: &str) -> bool {
    !id.is_empty()
        && id.len() <= 64
        && id
            .chars()
            .all(|ch| ch.is_ascii_alphanumeric() || ch == '_' || ch == '-')
}
