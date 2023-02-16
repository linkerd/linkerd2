#[derive(Clone, Debug, Eq, Hash, PartialEq)]
pub struct ResourceId {
    pub namespace: String,
    pub name: String,
}

impl ResourceId {
    pub fn new(namespace: String, name: String) -> Self {
        Self { namespace, name }
    }
}
