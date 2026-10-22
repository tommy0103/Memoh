//! `a11y-cli probe` subcommand — quickly verify that the accessibility bus is
//! reachable and that we can list children of the registry root. The Go caller
//! treats `ok: true` as the green light for ref-based Computer Use.

use anyhow::Result;
use serde::Serialize;

use crate::connection;

#[derive(Serialize)]
struct ProbeResult {
    ok: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    apps: Option<usize>,
    #[serde(skip_serializing_if = "Option::is_none")]
    bus_address: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    display: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    error: Option<String>,
    #[serde(skip_serializing_if = "Vec::is_empty")]
    discovery: Vec<String>,
}

pub async fn run() -> Result<()> {
    let outcome = probe().await;
    let result = match outcome {
        Ok(apps) => ProbeResult {
            ok: true,
            apps: Some(apps),
            bus_address: connection::current_bus_address(),
            display: std::env::var("DISPLAY").ok(),
            error: None,
            discovery: connection::discovery_log(),
        },
        Err(err) => ProbeResult {
            ok: false,
            apps: None,
            bus_address: connection::current_bus_address(),
            display: std::env::var("DISPLAY").ok(),
            error: Some(format!("{err:#}")),
            discovery: connection::discovery_log(),
        },
    };
    println!("{}", serde_json::to_string(&result)?);
    Ok(())
}

async fn probe() -> Result<usize> {
    let conn = connection::open().await?;
    let root = conn.root_accessible_on_registry().await?;
    let children = root.get_children().await?;
    Ok(children.len())
}
