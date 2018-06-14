use std::{
    cell::RefCell,
    fs,
    io,
    path::{Path, PathBuf},
    time::{Duration, Instant},
};

use futures::Stream;
use ring::digest::{self, Digest};

use tokio::timer::Interval;


/// Stream changes to the files at a group of paths.
pub fn stream_changes<I, P>(paths: I, interval: Duration)
    -> impl Stream<Item = (), Error = ()>
where
    I: IntoIterator<Item=P>,
    P: AsRef<Path>,
{
    // If we're on Linux, first atttempt to start an Inotify watch on the
    // paths. If this fails, fall back to polling the filesystem.
    #[cfg(target_os = "linux")] {
        stream_changes_inotify(paths, interval)
    }

    // If we're not on Linux, we can't use inotify, so simply poll the fs.
    // TODO: Use other FS events APIs (such as `kqueue`) as well, when
    //       they're available.
    #[cfg(not(target_os = "linux"))] {
        stream_changes_polling(paths, interval)
    }

}

/// Stream changes by polling the filesystem.
///
/// This will calculate the SHA-384 hash of each of files at the paths
/// described by this `CommonSettings` every `interval`, and attempt to
/// load a new `CommonConfig` from the files again if any of the hashes
/// has changed.
///
/// This is used on operating systems other than Linux, or on Linux if
/// our attempt to use `inotify` failed.
pub fn stream_changes_polling<I, P>(paths: I, interval: Duration)
    -> impl Stream<Item = (), Error = ()>
where
    I: IntoIterator<Item=P>,
    P: AsRef<Path>,
{
    let files = paths.into_iter()
        .map(PathAndHash::new)
        .collect::<Vec<_>>();

    Interval::new(Instant::now(), interval)
        .map_err(|e| error!("timer error: {:?}", e))
        .filter_map(move |_| {
            for file in &files  {
                match file.has_changed() {
                    Ok(true) => {
                        trace!("{:?} changed", &file.path);
                        return Some(());
                    },
                    Err(ref e) if e.kind() != io::ErrorKind::NotFound => {
                        // Ignore file not found errors so the log doesn't
                        // get too noisy.
                        warn!("error hashing {:?}: {}", &file.path, e);
                    },
                    _ => {
                        // If the file doesn't exist or the hash hasn't changed,
                        // keep going.
                    },
                }
            }
            None
        })
}


#[cfg(target_os = "linux")]
pub fn stream_changes_inotify<I, P>(paths: I, interval: Duration)
    -> impl Stream<Item = (), Error = ()>
where
    I: IntoIterator<Item=P>,
    P: AsRef<Path>,
{
    use ::stream;

    let paths: Vec<PathBuf> = paths.into_iter()
        .map(|p| p.as_ref().to_path_buf())
        .collect();
    let polls = Box::new(stream_changes_polling(paths.clone(), interval));
    match inotify::WatchStream::new(paths) {
        Ok(watch) => {
            let stream = inotify::FallbackStream {
                watch,
                polls,
            };
            stream::Either::A(stream)
        },
        Err(e) => {
            // If initializing the `Inotify` instance failed, it probably won't
            // succeed in the future (it's likely that inotify unsupported on
            // this OS).
            warn!("inotify init error: {}, falling back to polling", e);
            stream::Either::B(polls)
        },
    }
}

#[derive(Clone, Debug)]
struct PathAndHash {
    /// The path to the file.
    path: PathBuf,

    /// The last SHA-384 digest of the file, if we have previously hashed it.
    last_hash: RefCell<Option<Digest>>,
}

impl PathAndHash {
    fn new<P: AsRef<Path>>(path: P) -> Self {
        Self {
            path: path.as_ref().to_path_buf(),
            last_hash: RefCell::new(None),
        }
    }

    fn has_changed(&self) -> io::Result<bool> {
        let contents = fs::read(&self.path)?;
        let hash = Some(digest::digest(&digest::SHA256, &contents[..]));
        let changed = self.last_hash
            .borrow().as_ref()
            .map(Digest::as_ref) != hash.as_ref().map(Digest::as_ref);
        if changed {
            self.last_hash.replace(hash);
        }
        Ok(changed)
    }
}

#[cfg(target_os = "linux")]
pub mod inotify {
    use std::{
        io,
        path::PathBuf,
    };
    use inotify::{
        Inotify,
        Event,
        EventMask,
        EventStream,
        WatchMask,
    };
    use futures::{Async, Poll, Stream};

    pub struct WatchStream {
        inotify: Inotify,
        stream: EventStream,
        paths: Vec<PathBuf>,
    }

    pub struct FallbackStream {
        pub watch: WatchStream,
        pub polls: Box<Stream<Item = (), Error = ()> + Send>,
    }

    impl WatchStream {
        pub fn new(paths: Vec<PathBuf>) -> Result<Self, io::Error> {
            let mut inotify = Inotify::init()?;
            let stream = inotify.event_stream();

            let mut watch_stream = WatchStream {
                inotify,
                stream,
                paths,
            };

            watch_stream.add_paths()?;

            Ok(watch_stream)
        }


        fn add_paths(&mut self) -> Result<(), io::Error> {
            let mask
                = WatchMask::CREATE
                | WatchMask::MODIFY
                | WatchMask::DELETE
                | WatchMask::DELETE_SELF
                | WatchMask::MOVE
                | WatchMask::MOVE_SELF
                ;
            for path in &self.paths {
                let watch_path = path
                    .canonicalize()
                    .unwrap_or_else(|e| {
                        trace!("canonicalize({:?}): {:?}", &path, e);
                        path.parent()
                            .unwrap_or_else(|| path.as_ref())
                            .to_path_buf()
                    });
                self.inotify.add_watch(&watch_path, mask)?;
                trace!("watch {:?} (for {:?})", watch_path, path);
            }
            Ok(())
        }
    }

    impl Stream for WatchStream {
        type Item = ();
        type Error = io::Error;
        fn poll(&mut self) -> Poll<Option<Self::Item>, Self::Error> {
            loop {
                match try_ready!(self.stream.poll()) {
                    Some(Event { mask, name, .. }) => {
                        if mask.contains(EventMask::IGNORED) {
                            // This event fires if we removed a watch. Poll the
                            // stream again.
                            continue;
                        }
                        trace!("event={:?}; path={:?}", mask, name);
                        if mask.contains(
                            EventMask::DELETE & EventMask::DELETE_SELF & EventMask::CREATE
                        ) {
                            self.add_paths()?;
                        }
                        return Ok(Async::Ready(Some(())));
                    },
                    None => {
                        debug!("watch stream ending");
                        return Ok(Async::Ready(None));
                    },
                }
            }
        }
    }

    impl Stream for FallbackStream {
        type Item = ();
        type Error = ();
        fn poll(&mut self) -> Poll<Option<Self::Item>, Self::Error> {
            self.watch.poll().or_else(|e| {
                warn!("watch error: {:?}, polling the fs until next change", e);
                self.polls.poll()
            })
        }
    }

}
