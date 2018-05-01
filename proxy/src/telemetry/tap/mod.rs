use futures_mpsc_lossy;
use indexmap::IndexMap;

use conduit_proxy_controller_grpc::tap::observe_request;

use super::Event;

mod match_;

use self::match_::*;
pub use self::match_::InvalidMatch;

#[derive(Default, Debug)]
pub struct Taps {
    by_id: IndexMap<usize, Tap>,
}

#[derive(Debug)]
pub struct Tap {
    match_: Match,
    tx: futures_mpsc_lossy::Sender<Event>,
}

/// Indicates the tap is no longer receiving
struct Ended;

impl Taps {
    pub fn insert(&mut self, id: usize, tap: Tap) -> Option<Tap> {
        debug!("insert id={} tap={:?}", id, tap);
        self.by_id.insert(id, tap)
    }

    pub fn remove(&mut self, id: usize) -> Option<Tap> {
        debug!("remove id={}", id);
        self.by_id.swap_remove(&id)
    }

    ///
    pub(super) fn inspect(&mut self, ev: &Event) {
        if !ev.is_http() || self.by_id.is_empty() {
            return;
        }
        debug!("inspect taps={:?} event={:?}", self.by_id.keys().collect::<Vec<_>>(), ev);

        // Iterate through taps by index so that items may be removed.
        let mut idx = 0;
        while idx < self.by_id.len() {
            let (tap_id, inspect) = {
                let (id, tap) = self.by_id.get_index(idx).unwrap();
                (*id, tap.inspect(ev))
            };

            // If the tap is no longer receiving events, remove it. The index is only
            // incremented on successs so that, when an item is removed, the swapped item
            // is inspected on the next iteration OR, if the last item has been removed,
            // `len()` will return `idx` and a subsequent iteration will not occur.
            match inspect {
                Ok(matched) => {
                    debug!("inspect tap={} match={}", tap_id, matched);
                }
                Err(Ended) => {
                    debug!("ended tap={}", tap_id);
                    self.by_id.swap_remove_index(idx);
                    continue;
                }
            }

            idx += 1;
        }
    }
}

impl Tap {
    pub fn new(
        match_: &observe_request::Match,
        capacity: usize,
    ) -> Result<(Tap, futures_mpsc_lossy::Receiver<Event>), InvalidMatch> {
        let (tx, rx) = futures_mpsc_lossy::channel(capacity);
        let match_ = Match::new(match_)?;
        let tap = Tap {
            match_,
            tx,
        };
        Ok((tap, rx))
    }

    fn inspect(&self, ev: &Event) -> Result<bool, Ended> {
        if self.match_.matches(ev) {
            return self.tx
                .lossy_send(ev.clone())
                .map_err(|_| Ended)
                .map(|_| true);
        }

        Ok(false)
    }
}
