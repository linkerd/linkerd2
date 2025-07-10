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
                    write!(f, ".{part}")?;
                }
                Ok(())
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_star() {
        assert_eq!("*".parse(), Ok(IdentityMatch::Suffix(vec![])));

        assert_eq!(
            "*.example.com".parse(),
            Ok(IdentityMatch::Suffix(vec![
                "example".to_string(),
                "com".to_string()
            ]))
        );
        assert_eq!(
            "*.*.example.com".parse(),
            Ok(IdentityMatch::Suffix(vec![
                "*".to_string(),
                "example".to_string(),
                "com".to_string()
            ]))
        );
        assert_eq!(
            "x.example.com".parse(),
            Ok(IdentityMatch::Exact("x.example.com".to_string()))
        );

        assert_eq!(
            "**.example.com".parse(),
            Ok(IdentityMatch::Exact("**.example.com".to_string()))
        );

        assert_eq!(
            "foo.*.example.com".parse(),
            Ok(IdentityMatch::Exact("foo.*.example.com".to_string()))
        );
    }
}
