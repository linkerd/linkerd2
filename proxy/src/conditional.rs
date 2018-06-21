use std;

/// Like `std::option::Option<C>` but `None` carries a reason why the value
/// isn't available.
#[derive(Clone, Debug)]
pub enum Conditional<C, R>
where
    C: Clone + std::fmt::Debug,
    R: Clone + std::fmt::Debug,
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

impl<C, R> Eq for Conditional<C, R>
where
    C: Eq + Clone + std::fmt::Debug,
    R: Eq + Clone + std::fmt::Debug,
{
}

impl<C, R> PartialEq for Conditional<C, R>
where
    C: PartialEq + Clone + std::fmt::Debug,
    R: PartialEq + Clone + std::fmt::Debug,
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
    C: std::hash::Hash + Clone + std::fmt::Debug,
    R: std::hash::Hash + Clone + std::fmt::Debug,
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
    C: Clone + std::fmt::Debug,
    R: Copy + Clone + std::fmt::Debug,
{
    pub fn as_ref<'a>(&'a self) -> Conditional<&'a C, R> {
        match self {
            Conditional::Some(c) => Conditional::Some(&c),
            Conditional::None(r) => Conditional::None(*r),
        }
    }

    pub fn map<D, F>(self, f: F) -> Conditional<D, R>
    where
        F: FnOnce(C) -> D,
        D: Clone + std::fmt::Debug,
    {
        match self {
            Conditional::Some(c) => Conditional::Some(f(c)),
            Conditional::None(r) => Conditional::None(r),
        }
    }

    pub fn is_some(&self) -> bool {
        match self {
            Conditional::Some(_) => true,
            Conditional::None(_) => false,
        }
    }

}
