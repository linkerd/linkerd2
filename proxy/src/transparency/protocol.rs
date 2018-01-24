use httparse;

/// Known protocols that we proxy transparently.
#[derive(Debug)]
pub enum Protocol {
    Http1,
    Http2,
}

const H2_PREFACE: &[u8] = b"PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n";

impl Protocol {
    /// Tries to detect a known protocol in the peeked bytes.
    ///
    /// If no protocol can be determined, returns `None`.
    pub fn detect(bytes: &[u8]) -> Option<Protocol> {
        // http2 is easiest to detect
        if bytes.len() >= H2_PREFACE.len() {
            if &bytes[..H2_PREFACE.len()] == H2_PREFACE {
                return Some(Protocol::Http2);
            }
        }

        // http1 can have a really long first line, but if the bytes so far
        // look like http1, we'll assume it is. a different protocol
        // should look different in the first few bytes

        let mut headers = [httparse::EMPTY_HEADER; 0];
        let mut req = httparse::Request::new(&mut headers);
        match req.parse(bytes) {
            // Ok(Compelete) or Ok(Partial) both mean it looks like HTTP1!
            //
            // If we got past the first line, we'll see TooManyHeaders,
            // because we passed an array of 0 headers to parse into. That's fine!
            // We didn't want to keep parsing headers, just validate that
            // the first line is HTTP1.
            Ok(_) | Err(httparse::Error::TooManyHeaders) => {
                return Some(Protocol::Http1);
            },
            _ => {}
        }

        None
    }
}
