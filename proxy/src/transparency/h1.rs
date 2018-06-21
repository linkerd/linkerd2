use std::fmt::Write;
use std::mem;
use std::sync::Arc;

use bytes::BytesMut;
use http;
use http::header::{HOST, UPGRADE};
use http::uri::{Authority, Parts, Scheme, Uri};

use ctx::transport::{Server as ServerCtx};

/// Tries to make sure the `Uri` of the request is in a form needed by
/// hyper's Client.
pub fn normalize_our_view_of_uri<B>(req: &mut http::Request<B>) {
    // try to parse the Host header
    if let Some(auth) = authority_from_host(&req) {
        set_authority(req.uri_mut(), auth);
        return;
    }

    // last resort is to use the so_original_dst
    let orig_dst = req.extensions()
        .get::<Arc<ServerCtx>>()
        .and_then(|ctx| ctx.orig_dst_if_not_local());
    if let Some(orig_dst) = orig_dst {
        let mut bytes = BytesMut::with_capacity(31);
        write!(&mut bytes, "{}", orig_dst)
            .expect("socket address display is under 31 bytes");
        let bytes = bytes.freeze();
        let auth = Authority::from_shared(bytes)
            .expect("socket address is valid authority");
        set_authority(req.uri_mut(), auth);
    }
}

/// Returns an Authority from a request's Host header.
pub fn authority_from_host<B>(req: &http::Request<B>) -> Option<Authority> {
    req.headers().get(HOST)
        .and_then(|host| {
             host.to_str().ok()
                .and_then(|s| {
                    if s.is_empty() {
                        None
                    } else {
                        s.parse::<Authority>().ok()
                    }
                })
        })
}

fn set_authority(uri: &mut http::Uri, auth: Authority) {
    let mut parts = Parts::from(mem::replace(uri, Uri::default()));
    parts.scheme = Some(Scheme::HTTP);
    parts.authority = Some(auth);

    let new = Uri::from_parts(parts)
        .expect("absolute uri");

    *uri = new;
}

pub fn strip_connection_headers(headers: &mut http::HeaderMap) {
    let conn_val = if let Some(val) = headers.remove(http::header::CONNECTION) {
        val
    } else {
        return
    };

    let conn_header = if let Ok(s) = conn_val.to_str() {
        s
    } else {
        return
    };

    // A `Connection` header may have a comma-separated list of
    // names of other headers that are meant for only this specific connection.
    //
    // Iterate these names and remove them as headers.
    for name in conn_header.split(',') {
        let name = name.trim();
        headers.remove(name);
    }
}

/// Checks requests to determine if they want to perform an HTTP upgrade.
pub fn wants_upgrade<B>(req: &http::Request<B>) -> bool {
    // HTTP upgrades were added in 1.1, not 1.0.
    if req.version() != http::Version::HTTP_11 {
        return false;
    }

    if let Some(upgrade) = req.headers().get(UPGRADE) {
        // If an `h2` upgrade over HTTP/1.1 were to go by the proxy,
        // and it succeeded, there would an h2 connection, but it would
        // be opaque-to-the-proxy, acting as just a TCP proxy.
        //
        // A user wouldn't be able to see any usual HTTP telemetry about
        // requests going over that connection. Instead of that confusion,
        // the proxy strips h2 upgrade headers.
        //
        // Eventually, the proxy will support h2 upgrades directly.
        upgrade != "h2c"
    } else {
        // No Upgrade header means no upgrade wanted!
        false
    }


}

/// Checks responses to determine if they are successful HTTP upgrades.
pub fn is_upgrade<B>(res: &http::Response<B>) -> bool {
    // 101 Switching Protocols
    res.status() == http::StatusCode::SWITCHING_PROTOCOLS
        && res.version() == http::Version::HTTP_11
}
