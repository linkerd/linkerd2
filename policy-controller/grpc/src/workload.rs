use serde::{Deserialize, Serialize};
use std::str::FromStr;

#[derive(Clone, Debug, PartialEq, Eq, Deserialize, Serialize)]
pub enum Kind {
    #[serde(rename = "external_workload")]
    External(String),
    #[serde(rename = "pod")]
    Pod(String),
}

#[derive(Clone, Debug, PartialEq, Eq, Deserialize, Serialize)]
pub struct Workload {
    #[serde(flatten)]
    pub kind: Kind,
    #[serde(rename = "ns")]
    pub namespace: String,
}

impl FromStr for Workload {
    type Err = tonic::Status;

    fn from_str(s: &str) -> Result<Self, tonic::Status> {
        if s.starts_with('{') {
            return serde_json::from_str(s).map_err(|error| {
                tracing::warn!(%error, "Invalid {s} workload string");
                tonic::Status::invalid_argument(format!("Invalid workload: {s}"))
            });
        }

        match s.split_once(':') {
            None => Err(tonic::Status::invalid_argument(format!(
                "Invalid workload: {s}"
            ))),
            Some((ns, pod)) if ns.is_empty() || pod.is_empty() => Err(
                tonic::Status::invalid_argument(format!("Invalid workload: {s}")),
            ),
            Some((ns, pod)) => Ok(Workload {
                namespace: ns.to_string(),
                kind: Kind::Pod(pod.to_string()),
            }),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_old_format() {
        let input = "my-namespace:my-pod";
        let expected: Workload = Workload {
            namespace: "my-namespace".to_string(),
            kind: Kind::Pod("my-pod".to_string()),
        };
        assert_eq!(expected, Workload::from_str(input).expect("should parse"));
    }

    #[test]
    fn parse_new_format_pod() {
        let input = r#"{"ns":"my-namespace", "pod":"my-pod"}"#;
        let expected: Workload = Workload {
            namespace: "my-namespace".to_string(),
            kind: Kind::Pod("my-pod".to_string()),
        };
        assert_eq!(expected, Workload::from_str(input).expect("should parse"));
    }

    #[test]
    fn parse_new_format_external() {
        let input = r#"{"ns":"my-namespace", "external_workload":"my-external"}"#;
        let expected: Workload = Workload {
            namespace: "my-namespace".to_string(),
            kind: Kind::External("my-external".to_string()),
        };
        assert_eq!(expected, Workload::from_str(input).expect("should parse"));
    }

    #[test]
    fn errors_invalid_new_format() {
        let input = r#"{"ns":"my-namespace", "nonsense":"my-external"}"#;
        assert!(Workload::from_str(input).is_err());
    }
}
