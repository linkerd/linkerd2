use std::{
    sync::Arc,
    time::SystemTime,
};

use super::{
    ConfigError,

    ring::{self, rand, signature},
    rustls,
    untrusted,
    webpki,
};

pub struct CertResolver {
    certified_key: rustls::sign::CertifiedKey,
}

struct SigningKey {
    signer: Signer,
}

#[derive(Clone)]
struct Signer {
    private_key: Arc<signature::KeyPair>,
}

impl CertResolver {
    pub fn new(trust_anchors: &webpki::TLSServerTrustAnchors,
        cert_chain: Vec<rustls::Certificate>, private_key: untrusted::Input)
        -> Result<Self, ConfigError>
    {
        let now = webpki::Time::try_from(SystemTime::now())
            .map_err(|ring::error::Unspecified| ConfigError::TimeConversionFailed)?;

        // Verify that we were given a valid TLS certificate that was issued by
        // our CA.
        parse_end_entity_cert(&cert_chain)
            .and_then(|cert| {
                cert.verify_is_valid_tls_server_cert(
                    &[SIGNATURE_ALG_WEBPKI],
                    &trust_anchors,
                    &[], // No intermediate certificates
                    now)
                    .map_err(ConfigError::EndEntityCertIsNotValid)
            })?;

        let private_key = signature::key_pair_from_pkcs8(SIGNATURE_ALG_RING_SIGNING, private_key)
            .map_err(|ring::error::Unspecified| ConfigError::InvalidPrivateKey)?;

        let signer = Signer { private_key: Arc::new(private_key) };
        let signing_key = SigningKey { signer };
        let certified_key = rustls::sign::CertifiedKey::new(
            cert_chain, Arc::new(Box::new(signing_key)));
        Ok(Self { certified_key })
    }
}

fn parse_end_entity_cert<'a>(cert_chain: &'a[rustls::Certificate])
    -> Result<webpki::EndEntityCert<'a>, ConfigError>
{
    cert_chain.first()
        .ok_or(ConfigError::EmptyEndEntityCert)
        .and_then(|cert| {
            webpki::EndEntityCert::from(untrusted::Input::from(cert.as_ref()))
                .map_err(ConfigError::EndEntityCertIsNotValid)
        })
}

impl rustls::ResolvesServerCert for CertResolver {
    fn resolve(&self, server_name: Option<webpki::DNSNameRef>,
               sigschemes: &[rustls::SignatureScheme]) -> Option<rustls::sign::CertifiedKey> {
        let server_name = server_name?; // Require SNI

        if !sigschemes.contains(&SIGNATURE_ALG_RUSTLS_SCHEME) {
            return None;
        }

        // Verify that our certificate is valid for the given SNI name.
        parse_end_entity_cert(&self.certified_key.cert).ok()
            .and_then(|cert| cert.verify_is_valid_for_dns_name(server_name).ok())?;

        Some(self.certified_key.clone())
    }
}

impl rustls::sign::SigningKey for SigningKey {
    fn choose_scheme(&self, offered: &[rustls::SignatureScheme])
        -> Option<Box<rustls::sign::Signer>>
    {
        if offered.contains(&SIGNATURE_ALG_RUSTLS_SCHEME) {
            Some(Box::new(self.signer.clone()))
        } else {
            None
        }
    }

    fn algorithm(&self) -> rustls::internal::msgs::enums::SignatureAlgorithm {
        SIGNATURE_ALG_RUSTLS_ALGORITHM
    }
}

impl rustls::sign::Signer for Signer {
    fn sign(&self, message: &[u8]) -> Result<Vec<u8>, rustls::TLSError> {
        let rng = rand::SystemRandom::new();
        signature::sign(&self.private_key, &rng, untrusted::Input::from(message))
            .map(|signature| signature.as_ref().to_owned())
            .map_err(|ring::error::Unspecified|
                rustls::TLSError::General("Signing Failed".to_owned()))
    }

    fn get_scheme(&self) -> rustls::SignatureScheme {
        SIGNATURE_ALG_RUSTLS_SCHEME
    }
}

// Keep these in sync.
static SIGNATURE_ALG_RING_SIGNING: &signature::SigningAlgorithm =
    &signature::ECDSA_P256_SHA256_ASN1_SIGNING;
const SIGNATURE_ALG_RUSTLS_SCHEME: rustls::SignatureScheme =
    rustls::SignatureScheme::ECDSA_NISTP256_SHA256;
const SIGNATURE_ALG_RUSTLS_ALGORITHM: rustls::internal::msgs::enums::SignatureAlgorithm =
    rustls::internal::msgs::enums::SignatureAlgorithm::ECDSA;
static SIGNATURE_ALG_WEBPKI: &webpki::SignatureAlgorithm = &webpki::ECDSA_P256_SHA256;
