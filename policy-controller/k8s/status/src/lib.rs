mod http_route;
mod index;
mod resource_id;

#[cfg(test)]
mod tests;

pub use self::index::{Controller, Index, STATUS_CONTROLLER_NAME};
