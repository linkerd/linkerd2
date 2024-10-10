pub mod index;

pub use index::{metrics, Index, ResourceRef, SharedIndex};

#[cfg(test)]
mod tests;
