use std::{fmt, io};

use super::{Counter, Gauge, Metric};

#[derive(Copy, Clone)]
pub struct ProcessMetrics {
    cpu_seconds_total: Counter,
    open_fds: Gauge,
    // max_fds: Gauge, // TODO: support this
    virtual_memory_bytes: Gauge,
    resident_memory_bytes: Gauge,
}

impl ProcessMetrics {
    metrics! {
        process_cpu_seconds_total: Counter {
            "Total user and system CPU time spent in seconds."
        },
        process_open_fds: Gauge { "Number of open file descriptors." },
        // process_max_fds: Gauge { "Maximum number of open file descriptors." },
        process_virtual_memory_bytes: Gauge {
            "Virtual memory size in bytes."
        },
        process_resident_memory_bytes: Gauge {
            "Resident memory size in bytes."
        }
    }

    pub fn collect() -> io::Result<Self> {
        self::imp::collect()
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

        // Self::process_max_fds.fmt_help(f)?;
        // Self::process_max_fds.fmt_metric(f, self.max_fds)?;

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
    use super::ProcessMetrics;
    use super::super::{Counter, Gauge};

    use std::{io, fs};

    use procinfo::pid;
    use libc::pid_t;

    pub fn collect() -> io::Result<ProcessMetrics> {
        let stat = pid::stat_self()?;

        let cpu_seconds_total =
            Counter::from((stat.utime + stat.stime) as u64);
        let virtual_memory_bytes = Gauge::from(stat.vsize as u64);
        let resident_memory_bytes = Gauge::from(stat.rss as u64);

        let metrics = ProcessMetrics {
            cpu_seconds_total: cpu_seconds_total,
            open_fds: open_fds(stat.pid)?,
            resident_memory_bytes,
            virtual_memory_bytes,
        };
        Ok(metrics)
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
}

#[cfg(not(target_os = "linux"))]
mod imp {
    use super::ProcessMetrics;
    use std::io;

    pub fn collect() -> io::Result<ProcessMetrics> {
        Err(io::Error::new(
            io::ErrorKind::Other,
            "procinfo not supported on this operating system"
        ))
    }
}
