use std;

/// Like `std::option::Option<C>` but `None` carries a reason why the value
/// isn't available.
#[derive(Clone)]
pub enum Conditional<C, R>
where
    C: Clone,
    R: Clone,
{
    Some(C),
    None(R),
}

impl<C, R> Copy for Conditional<C, R>
where
    C: Copy + Clone + std::fmt::Debug,
    R: Copy + Clone + std::fmt::Debug,
{
}

impl<C, R> std::fmt::Debug for Conditional<C, R>
where
    C: Clone + std::fmt::Debug,
    R: Clone + std::fmt::Debug
{
    fn fmt(&self, f: &mut std::fmt::Formatter) -> Result<(), std::fmt::Error> {
        match self {
            Conditional::Some(s) => f.debug_tuple("Some").field(s).finish(),
            Conditional::None(r) => f.debug_tuple("None").field(r).finish(),
        }
    }
}

impl<C, R> Eq for Conditional<C, R>
where
    C: Eq + Clone,
    R: Eq + Clone,
{
}

impl<C, R> PartialEq for Conditional<C, R>
where
    C: PartialEq + Clone,
    R: PartialEq + Clone,
{
    fn eq(&self, other: &Conditional<C, R>) -> bool {
        use self::Conditional::*;
        match (self, other) {
            (Some(a), Some(b)) => a.eq(b),
            (None(a), None(b)) => a.eq(b),
            _ => false,
        }
    }
}

impl<C, R> std::hash::Hash for Conditional<C, R>
where
    C: std::hash::Hash + Clone,
    R: std::hash::Hash + Clone,
{
    fn hash<H: std::hash::Hasher>(&self, state: &mut H) {
        match self {
            Conditional::Some(c) => c.hash(state),
            Conditional::None(r) => r.hash(state),
        }
    }
}

impl<C, R> Conditional<C, R>
where
    C: Clone,
    R: Copy + Clone,
{
    pub fn as_ref<'a>(&'a self) -> Conditional<&'a C, R> {
        match self {
            Conditional::Some(c) => Conditional::Some(&c),
            Conditional::None(r) => Conditional::None(*r),
        }
    }
}
