//! Traits for conversions between types.
//!
//! Currently, this module reimplements the `TryFrom` and `TryInto`
//! traits as these are not yet stable in the standard library.
//!
//! # Generic Implementations

/// Private trait for generic methods with fallible conversions.
///
/// This trait is similar to the `TryFrom` trait proposed in the standard
/// library, and should be removed when `TryFrom` is stabilized.
pub trait TryFrom<T>: Sized {
    type Err;

    #[doc(hidden)]
    fn try_from(t: T) -> Result<Self, Self::Err>;
}
