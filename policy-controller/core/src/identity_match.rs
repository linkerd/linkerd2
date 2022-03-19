use std::{convert::Infallible, fmt, str::FromStr};

/// Matches a client's mesh identity.
#[derive(Clone, Debug, PartialEq, Eq, Hash)]
pub enum IdentityMatch {
    /// An exact match.
    Exact(String),

    /// A suffix match.
    Suffix(Vec<String>),
}

// === impl IdentityMatch ===

impl FromStr for IdentityMatch {
    type Err = Infallible;

    fn from_str(s: &str) -> Result<Self, Infallible> {
        if s == "*" {
            return Ok(IdentityMatch::Suffix(vec![]));
        }

        if s.starts_with("*.") {
            return Ok(IdentityMatch::Suffix(
                s.split('.').skip(1).map(|s| s.to_string()).collect(),
            ));
        }

        Ok(IdentityMatch::Exact(s.to_string()))
    }
}

impl fmt::Display for IdentityMatch {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        use std::fmt::Write;
        match self {
            Self::Exact(name) => name.fmt(f),
            Self::Suffix(suffix) => {
                f.write_char('*')?;
                for part in suffix {
                    write!(f, ".{}", part)?;
                }
                Ok(())
            }
        }
    }
}
