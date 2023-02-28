mod http_route;
pub mod index;
mod resource_id;

#[cfg(test)]
mod tests;

pub use self::index::{Controller, Index};
