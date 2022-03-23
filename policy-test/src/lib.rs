#![deny(warnings, rust_2018_idioms)]
#![forbid(unsafe_code)]

pub mod admission;
pub mod grpc;

use linkerd_policy_controller_k8s_api as k8s;
use rand::Rng;
use tracing::Instrument;

/// Runs a test with a random namespace that is deleted on test completion
pub async fn with_temp_ns<F, Fut>(test: F)
where
    F: FnOnce(kube::Client, String) -> Fut,
    Fut: std::future::Future<Output = ()> + Send + 'static,
{
    let _tracing = init_tracing();

    let namespace = {
        // TODO(ver) include the test name in this string?
        format!("linkerd-policy-test-{}", random_suffix(6))
    };

    tracing::debug!("initializing client");
    let client = kube::Client::try_default()
        .await
        .expect("failed to initialize k8s client");
    let api = kube::Api::<k8s::Namespace>::all(client.clone());

    tracing::debug!(%namespace, "creating");
    let ns = k8s::Namespace {
        metadata: k8s::ObjectMeta {
            name: Some(namespace.clone()),
            ..Default::default()
        },
        ..Default::default()
    };
    api.create(&kube::api::PostParams::default(), &ns)
        .await
        .expect("failed to create Namespace");

    tracing::trace!("spawning");
    let test = test(client.clone(), namespace.clone());
    let res = tokio::spawn(test.instrument(tracing::info_span!("test", %namespace))).await;
    if res.is_err() {
        // If the test failed, stop tracing so the log is not polluted with more information about
        // cleanup after the failure was printed.
        drop(_tracing);
    }

    tracing::debug!(%namespace, "deleting");
    api.delete(&namespace, &kube::api::DeleteParams::background())
        .await
        .expect("failed to delete Namespace");
    if let Err(err) = res {
        std::panic::resume_unwind(err.into_panic());
    }
}

pub fn random_suffix(len: usize) -> String {
    let rng = &mut rand::thread_rng();
    (0..len)
        .map(|_| rng.sample(LowercaseAlphanumeric) as char)
        .collect()
}

fn init_tracing() -> tracing::subscriber::DefaultGuard {
    tracing::subscriber::set_default(
        tracing_subscriber::fmt()
            .with_test_writer()
            .with_max_level(tracing::Level::TRACE)
            .finish(),
    )
}

struct LowercaseAlphanumeric;

// Modified from `rand::distributions::Alphanumeric`
//
// Copyright 2018 Developers of the Rand project
// Copyright (c) 2014 The Rust Project Developers
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
impl rand::distributions::Distribution<u8> for LowercaseAlphanumeric {
    fn sample<R: Rng + ?Sized>(&self, rng: &mut R) -> u8 {
        const RANGE: u32 = 26 + 10;
        const CHARSET: &[u8] = b"abcdefghijklmnopqrstuvwxyz0123456789";
        loop {
            let var = rng.next_u32() >> (32 - 6);
            if var < RANGE {
                return CHARSET[var as usize];
            }
        }
    }
}
