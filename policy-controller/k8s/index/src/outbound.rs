pub mod index;

pub use index::{metrics, Index, ParentRef, SharedIndex};

#[cfg(test)]
mod tests;
