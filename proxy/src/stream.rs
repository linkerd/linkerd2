use futures::{Poll, Stream};

/// Like `futures::future::Either` but for `Stream`s.
///
/// Combines two different `Stream`s yielding the same item and error
/// types into a single type.
///
// TODO: This is probably useful outside of Conduit as well. Perhaps it
//       deserves to be in a library...
#[derive(Clone, Debug)]
pub enum Either<A, B> {
    A(A),
    B(B),
}

// We could implement this function using the exact same code as
// `futures::future::Either`, if we needed it. I've commented it
// out, as we currently don't need it.
// impl<T, A, B> Either<(T, A), (T, B)> {
//     /// Splits out the homogeneous type from an either of tuples.
//     pub fn split(self) -> (T, Either<A, B>) {
//         match self {
//             Either::A((a, b)) => (a, Either::A(b)),
//             Either::B((a, b)) => (a, Either::B(b)),
//         }
//     }
// }

impl<A, B> Stream for Either<A, B>
where
    A: Stream,
    B: Stream<Item = A::Item, Error = A::Error>,
{
    type Item = A::Item;
    type Error = A::Error;

    fn poll(&mut self) -> Poll<Option<A::Item>, A::Error> {
        match *self {
            Either::A(ref mut a) => a.poll(),
            Either::B(ref mut b) => b.poll(),
        }
    }
}
