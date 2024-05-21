pub mod index;

pub use index::{metrics, Index, ServiceRef, SharedIndex};

#[cfg(test)]
mod tests;
