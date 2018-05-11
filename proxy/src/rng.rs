use rand;

/// An empty type which implements `rand::Rng` by lazily getting the current
/// `thread_rng` when its' called.
///
/// This can be used in cases where we need a type to be `Send`, but wish to
/// use the thread-local RNG.
#[derive(Copy, Clone, Debug, Default)]
pub struct LazyThreadRng;

// ===== impl LazyRng =====

impl rand::Rng for LazyThreadRng {
    fn next_u32(&mut self) -> u32 {
        rand::thread_rng().next_u32()
    }

    fn next_u64(&mut self) -> u64 {
        rand::thread_rng().next_u64()
    }

    #[inline]
    fn fill_bytes(&mut self, bytes: &mut [u8]) {
        rand::thread_rng().fill_bytes(bytes)
    }
}
