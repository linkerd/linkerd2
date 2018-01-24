use std::fmt::Write;
use std::mem;
use std::sync::Arc;

use bytes::BytesMut;
use http;
use http::header::{HeaderValue, HOST};
use http::uri::{Authority, Parts, Scheme, Uri};

use ctx::transport::{Server as ServerCtx};

pub fn reconstruct_uri<B>(req: &mut http::Request<B>) -> Result<(), ()> {
    // RFC7230#section-5.4
    // If an absolute-form uri is received, it must replace
    // the host header
    if let Some(auth) = req.uri().authority_part().cloned() {
        if let Some(host) = req.headers().get(HOST) {
            if auth.as_str().as_bytes() == host.as_bytes() {
                // host and absolute-form agree, nothing more to do
                return Ok(());
            }
        }
        let host = HeaderValue::from_shared(auth.into_bytes())
            .expect("a valid authority is valid header value");
        req.headers_mut().insert(HOST, host);
        return Ok(());
    }

    // try to parse the Host header
    if let Some(host) = req.headers().get(HOST).cloned() {
        let auth = host.to_str()
            .ok()
            .and_then(|s| {
                if s.is_empty() {
                    None
                } else {
                    s.parse::<Authority>().ok()
                }
            });
        if let Some(auth) = auth {
            set_authority(req.uri_mut(), auth);
            return Ok(());
        }
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

        return Ok(());
    }

    Err(())
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


