use schemars::JsonSchema;
use serde::{Deserialize, Serialize};
use std::{
    collections::{BTreeMap, BTreeSet},
    sync::Arc,
};

#[derive(Clone, Debug, Eq, Default)]
pub struct Labels(Arc<Map>);

pub type Map = BTreeMap<String, String>;

pub type Expressions = Vec<Expression>;

#[derive(Clone, Debug, PartialEq, Eq, Deserialize, Serialize, JsonSchema)]
pub struct Expression {
    key: String,
    operator: Operator,
    values: BTreeSet<String>,
}

#[derive(Clone, Debug, PartialEq, Eq, Deserialize, Serialize, JsonSchema)]
pub enum Operator {
    In,
    NotIn,
}

/// Selects a set of pods that expose a server.
#[derive(Clone, Debug, Eq, PartialEq, Default, Deserialize, Serialize, JsonSchema)]
#[serde(rename_all = "camelCase")]
pub struct Selector {
    match_labels: Option<Map>,
    match_expressions: Option<Expressions>,
}

// === Selector ===

impl Selector {
    pub fn from_expressions(exprs: Expressions) -> Self {
        Self {
            match_labels: None,
            match_expressions: Some(exprs),
        }
    }

    pub fn from_map(map: Map) -> Self {
        Self {
            match_labels: Some(map),
            match_expressions: None,
        }
    }

    pub fn matches(&self, labels: &Labels) -> bool {
        for expr in self.match_expressions.iter().flatten() {
            if !expr.matches(labels.as_ref()) {
                return false;
            }
        }

        if let Some(match_labels) = self.match_labels.as_ref() {
            for (k, v) in match_labels.iter() {
                if labels.0.get(k) != Some(v) {
                    return false;
                }
            }
        }

        true
    }
}

impl std::iter::FromIterator<(String, String)> for Selector {
    fn from_iter<T: IntoIterator<Item = (String, String)>>(iter: T) -> Self {
        Self::from_map(iter.into_iter().collect())
    }
}

impl std::iter::FromIterator<(&'static str, &'static str)> for Selector {
    fn from_iter<T: IntoIterator<Item = (&'static str, &'static str)>>(iter: T) -> Self {
        Self::from_map(
            iter.into_iter()
                .map(|(k, v)| (k.to_string(), v.to_string()))
                .collect(),
        )
    }
}

impl std::iter::FromIterator<Expression> for Selector {
    fn from_iter<T: IntoIterator<Item = Expression>>(iter: T) -> Self {
        Self::from_expressions(iter.into_iter().collect())
    }
}

// === Labels ===

impl From<Map> for Labels {
    #[inline]
    fn from(labels: Map) -> Self {
        Self(Arc::new(labels))
    }
}

impl AsRef<Map> for Labels {
    #[inline]
    fn as_ref(&self) -> &Map {
        self.0.as_ref()
    }
}

impl<T: AsRef<Map>> std::cmp::PartialEq<T> for Labels {
    #[inline]
    fn eq(&self, t: &T) -> bool {
        self.0.as_ref().eq(t.as_ref())
    }
}

impl std::iter::FromIterator<(String, String)> for Labels {
    fn from_iter<T: IntoIterator<Item = (String, String)>>(iter: T) -> Self {
        Self(Arc::new(iter.into_iter().collect()))
    }
}

impl std::iter::FromIterator<(&'static str, &'static str)> for Labels {
    fn from_iter<T: IntoIterator<Item = (&'static str, &'static str)>>(iter: T) -> Self {
        iter.into_iter()
            .map(|(k, v)| (k.to_string(), v.to_string()))
            .collect()
    }
}

// === Expression ===

impl Expression {
    fn matches(&self, labels: &Map) -> bool {
        match self.operator {
            Operator::In => {
                if let Some(v) = labels.get(&self.key) {
                    return self.values.contains(v);
                }
            }
            Operator::NotIn => {
                return match labels.get(&self.key) {
                    Some(v) => self.values.contains(v),
                    None => true,
                }
            }
        }

        false
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::iter::FromIterator;

    #[test]
    fn test_matches() {
        for (selector, labels, matches, msg) in &[
            (Selector::default(), Labels::default(), true, "empty match"),
            (
                Selector::from_iter(Some(("foo", "bar"))),
                Labels::from_iter(Some(("foo", "bar"))),
                true,
                "exact label match",
            ),
            (
                Selector::from_iter(Some(("foo", "bar"))),
                Labels::from_iter(vec![("foo", "bar"), ("bah", "baz")]),
                true,
                "sufficient label match",
            ),
            (
                Selector::from_iter(Some(Expression {
                    key: "foo".into(),
                    operator: Operator::In,
                    values: Some("bar".to_string()).into_iter().collect(),
                })),
                Labels::from_iter(vec![("foo", "bar"), ("bah", "baz")]),
                true,
                "expression match",
            ),
        ] {
            assert_eq!(selector.matches(labels), *matches, "{}", msg);
        }
    }
}
