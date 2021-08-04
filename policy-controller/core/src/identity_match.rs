use std::fmt;

/// Matches a client's mesh identity.
#[derive(Clone, Debug, PartialEq, Eq, Hash)]
pub enum IdentityMatch {
    /// An exact match.
    Name(String),

    /// A suffix match.
    Suffix(Vec<String>),
}

// === impl IdentityMatch ===

impl fmt::Display for IdentityMatch {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        use std::fmt::Write;
        match self {
            Self::Name(name) => name.fmt(f),
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
