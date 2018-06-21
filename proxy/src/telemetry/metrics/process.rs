use std::fmt;
use super::{Counter, Gauge, Metric};

pub use self::imp::Sensor;

#[derive(Copy, Clone)]
pub struct ProcessMetrics {
    cpu_seconds_total: Counter,
    open_fds: Gauge,
    max_fds: Option<Gauge>,
    virtual_memory_bytes: Gauge,
    resident_memory_bytes: Gauge,
}

impl ProcessMetrics {
    metrics! {
        process_cpu_seconds_total: Counter {
            "Total user and system CPU time spent in seconds."
        },
        process_open_fds: Gauge { "Number of open file descriptors." },
        process_max_fds: Gauge { "Maximum number of open file descriptors." },
        process_virtual_memory_bytes: Gauge {
            "Virtual memory size in bytes."
        },
        process_resident_memory_bytes: Gauge {
            "Resident memory size in bytes."
        }
    }
}

impl fmt::Display for ProcessMetrics {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        Self::process_cpu_seconds_total.fmt_help(f)?;
        Self::process_cpu_seconds_total.fmt_metric(
            f,
            self.cpu_seconds_total
        )?;

        Self::process_open_fds.fmt_help(f)?;
        Self::process_open_fds.fmt_metric(f, self.open_fds)?;

        if let Some(ref max_fds) = self.max_fds {
            Self::process_max_fds.fmt_help(f)?;
            Self::process_max_fds.fmt_metric(f, *max_fds)?;
        }

        Self::process_virtual_memory_bytes.fmt_help(f)?;
        Self::process_virtual_memory_bytes.fmt_metric(
            f,
            self.virtual_memory_bytes
        )?;

        Self::process_resident_memory_bytes.fmt_help(f)?;
        Self::process_resident_memory_bytes.fmt_metric(
            f,
            self.resident_memory_bytes
        )
    }
}

#[cfg(target_os = "linux")]
mod imp {
    use super::*;
    use super::super::{Counter, Gauge};

    use std::{io, fs};

    use procinfo::pid;
    use libc::{self, pid_t};

    #[derive(Debug)]
    pub struct Sensor {
        page_size: usize,
    }

    impl Sensor {
        pub fn new() -> io::Result<Sensor> {
            let page_size = match unsafe { libc::sysconf(libc::_SC_PAGESIZE) } {
                e if e < 0 => {
                    let error = io::Error::last_os_error();
                    error!("error getting page size: {:?}", error);
                    return Err(error);
                },
                page_size => page_size as usize,
            };
            Ok(Sensor {
                page_size,
            })
        }

        pub fn metrics(&self) -> io::Result<ProcessMetrics> {
            // XXX potentially blocking call
            let stat = pid::stat_self()?;

            let cpu_seconds_total = Counter::from((stat.utime + stat.stime) as u64);
            let virtual_memory_bytes = Gauge::from(stat.vsize as u64);
            let resident_memory_bytes = Gauge::from((stat.rss * self.page_size) as u64);

            let metrics = ProcessMetrics {
                cpu_seconds_total,
                virtual_memory_bytes,
                resident_memory_bytes,
                open_fds: open_fds(stat.pid)?,
                max_fds: max_fds()?,
            };

            Ok(metrics)
        }
    }


    fn open_fds(pid: pid_t) -> io::Result<Gauge> {
        let mut open = 0;
        for f in fs::read_dir(format!("/proc/{}/fd", pid))? {
            if !f?.file_type()?.is_dir() {
                open += 1;
            }
        }
        Ok(Gauge::from(open))
    }

    fn max_fds() -> io::Result<Option<Gauge>> {
        let limit = pid::limits_self()?.max_open_files;
        let max_fds = limit.soft.or(limit.hard)
            .map(|max| Gauge::from(max as u64));
        Ok(max_fds)
    }
}

#[cfg(not(target_os = "linux"))]
mod imp {
    use super::*;
    use std::io;

    #[derive(Debug)]
    pub struct Sensor {}

    impl Sensor {
        pub fn new() -> io::Result<Sensor> {
            Err(io::Error::new(
                io::ErrorKind::Other,
                "procinfo not supported on this operating system"
            ))
        }

        pub fn metrics(&self) -> io::Result<ProcessMetrics> {
            unreachable!("process::Sensor::metrics() on unsupported OS!")
        }
    }

}
