mod index;
mod resource_id;
mod routes;
mod service;

#[cfg(test)]
mod tests;

pub use self::index::{Controller, ControllerMetrics, Index, IndexMetrics};
