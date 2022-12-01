use linkerd_policy_controller_k8s_api as k8s;
use linkerd_policy_controller_k8s_index as index;
use tokio::sync::mpsc;

pub async fn process_patches(
    client: k8s::Client,
    mut patches_rx: mpsc::UnboundedReceiver<index::Patch>,
) {
    let patch_params = k8s::PatchParams::apply("policy.linkerd.io");
    while let Some(patch) = patches_rx.recv().await {
        let index::Patch {
            name,
            namespace,
            value,
        } = patch;
        tracing::info!(%value, "Patching HTTPRoute");
        let api = k8s::Api::<k8s::policy::HttpRoute>::namespaced(client.clone(), &namespace);
        if let Err(error) = api
            .patch_status(&name, &patch_params, &k8s::Patch::Merge(&value))
            .await
        {
            tracing::info!(%error)
        }
    }
}
