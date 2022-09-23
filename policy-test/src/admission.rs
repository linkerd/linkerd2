use crate::with_temp_ns;

pub async fn accepts<F, T>(f: F)
where
    F: FnOnce(String) -> T + Send + 'static,
    T: Clone
        + Send
        + Sync
        + std::fmt::Debug
        + kube::Resource<Scope = kube::core::NamespaceResourceScope>
        + serde::de::DeserializeOwned
        + serde::Serialize,
    T::DynamicType: Default,
{
    with_temp_ns(|client, ns| async move {
        let api = kube::Api::namespaced(client, &*ns);
        let obj = f(ns);
        let res = api.create(&kube::api::PostParams::default(), &obj).await;
        res.expect("resource must apply");
    })
    .await;
}

pub async fn rejects<F, T>(f: F)
where
    F: FnOnce(String) -> T + Send + 'static,
    T: Clone
        + Send
        + Sync
        + std::fmt::Debug
        + kube::Resource<Scope = kube::core::NamespaceResourceScope>
        + serde::de::DeserializeOwned
        + serde::Serialize,
    T::DynamicType: Default,
{
    with_temp_ns(|client, ns| async move {
        let api = kube::Api::namespaced(client, &*ns);
        let obj = f(ns);
        let res = api.create(&kube::api::PostParams::default(), &obj).await;
        res.expect_err("resource must not apply");
    })
    .await;
}
