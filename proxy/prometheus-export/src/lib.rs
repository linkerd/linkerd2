extern crate prost;
#[macro_use]
extern crate prost_derive;

use std::{fmt, f64};
// pub use self::pb::*;

/// Prometheus Protocol Buffers export format.
pub mod pb {
    include!(concat!(env!("OUT_DIR"), "/io.prometheus.client.rs"));
}

#[derive(Debug, Clone, PartialEq)]
pub enum MetricValue<'a> {
    Counter(&'a pb::Counter),
    Histogram(&'a pb::Histogram),
    Gauge(&'a pb::Gauge),
    Untyped(&'a pb::Untyped),
    Summary(&'a pb::Summary),
}

// ===== impl MetricValue =====
impl<'a> fmt::Display for MetricValue<'a> {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        match *self {
            MetricValue::Counter(ref v) =>
                fmt::Display::fmt(&DisplayFloat(v.value()), f),
            MetricValue::Gauge(ref v) =>
                fmt::Display::fmt(&DisplayFloat(v.value()), f),
             MetricValue::Untyped(ref v) =>
                fmt::Display::fmt(&DisplayFloat(v.value()), f),
            _ => // histograms and summaries have to be special-cased.
                unimplemented!(),
        }
    }
}

// ===== impl MetricFamily =====

impl fmt::Display for pb::MetricFamily {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        let name = self.name();
        if let Some(ref help) = self.help.as_ref() {
            write!(f, "# HELP {name} {help}\n", name=name, help=help)?;
        }

        write!(f, "# TYPE {name} {ty}\n", name=name, ty=self.type_())?;

        for ref metric in &self.metric {
            let labels = DisplayLabels(&metric.label[..]);
            match metric.value() {
                MetricValue::Histogram(ref histogram) => {
                    let display = DisplayHistogram {
                        name, labels, histogram,
                    };
                    fmt::Display::fmt(&display, f)
                },
                MetricValue::Summary(ref summary) => {
                    let display = DisplaySummary {
                        name, labels, summary,
                    };
                    fmt::Display::fmt(&display, f)
                },
                value => {
                    if labels.0.is_empty() {
                        write!(f, "{} {}", name, value)
                    } else {
                        write!(f, "{name}{{{labels}}} {value}",
                            name = name,
                            labels = labels,
                            value = value,
                        )
                    }?;

                    if let Some(ref timestamp) = metric.timestamp_ms {
                        write!(f, " {}\n", timestamp)
                    } else {
                        write!(f, "\n")
                    }
                }
            }?;
        }

        Ok(())
    }
}

/// Format a histogram.
///
/// This struct is necessary as a histogram must know its' name and labels.
#[derive(Clone, Debug)]
struct DisplayHistogram<'a> {
    name: &'a str,
    labels: DisplayLabels<'a>,
    histogram: &'a pb::Histogram,
}

impl<'a> fmt::Display for DisplayHistogram<'a> {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        let &DisplayHistogram { ref name, ref labels, ref histogram } = self;
        let has_labels = labels.0.is_empty();
        for ref bucket in &histogram.bucket {
            write!(f, "{name}_bucket{{{labels}{comma}le=\"{le}\"}} {value}\n",
                name = name,
                labels = labels,
                comma = if has_labels { "," } else { "" },
                le = DisplayFloat(bucket.upper_bound()),
                value = bucket.cumulative_count(),
            )?;
        }

        if has_labels {
            // if there are labels, format them inside of curly braces.
            write!(f,
                "{name}_sum{{{labels}}} {sum}\n\
                {name}_count{{{labels}}} {count}\n",
                name = name,
                labels = labels,
                sum = DisplayFloat(histogram.sample_sum()),
                count = histogram.sample_count()
            )?;
        } else {
            // otherwise, skip the curly braces.
            write!(f, "{name}_sum {sum}\n{name}_count {count}\n",
                name = name,
                sum = DisplayFloat(histogram.sample_sum()),
                count = histogram.sample_count(),
            )?;
        }

        Ok(())
    }

}

/// Format a summary.
///
/// This struct is necessary as a summary must know its' name and labels.
#[derive(Clone, Debug)]
struct DisplaySummary<'a> {
    name: &'a str,
    labels: DisplayLabels<'a>,
    summary: &'a pb::Summary,
}

impl<'a> fmt::Display for DisplaySummary<'a> {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        let &DisplaySummary { ref name, ref labels, ref summary } = self;
        let has_labels = labels.0.is_empty();
        for ref quantile in &summary.quantile {
            write!(f,
                "{name}{{{labels}{comma}quantile=\"{quantile}\"}} {value}\n",
                name = name,
                labels = labels,
                comma = if has_labels { "," } else { "" },
                quantile = DisplayFloat(quantile.quantile()),
                value = DisplayFloat(quantile.value()),
            )?;
        }

        if has_labels {
            // if there are labels, format them inside of curly braces.
            write!(f,
                "{name}_sum{{{labels}}} {sum}\n\
                {name}_count{{{labels}}} {count}\n",
                name = name,
                labels = labels,
                sum = DisplayFloat(summary.sample_sum()),
                count = summary.sample_count()
            )?;
        } else {
            // otherwise, skip the curly braces.
            write!(f, "{name}_sum {sum}\n{name}_count {count}",
                name = name,
                sum = DisplayFloat(summary.sample_sum()),
                count = summary.sample_count(),
            )?;
        }

        Ok(())
    }

}

// ===== impl Metric =====

impl pb::Metric {
    pub fn value(&self) -> MetricValue {
        match *self {
            pb::Metric {
                counter: Some(ref v), histogram: None, gauge: None,
                untyped: None, summary: None, ..
            } => MetricValue::Counter(v),
            pb::Metric {
                counter: None, histogram: Some(ref v), gauge: None,
                untyped: None, summary: None, ..
            } => MetricValue::Histogram(v),
            pb::Metric {
                counter: None, histogram: None, gauge: Some(ref v),
                untyped: None, summary: None, ..
            } => MetricValue::Gauge(v),
            pb::Metric {
                counter: None, histogram: None, gauge: None,
                untyped: Some(ref v), summary: None, ..
            } => MetricValue::Untyped(v),
            pb::Metric {
                counter: None, histogram: None, gauge: None,
                untyped: None, summary: Some(ref v), ..
            } => MetricValue::Summary(v),
            _ => panic!("weirdly shaped metrics: {:?}", self),
        }
    }
}

// ===== impl MetricType =====

impl fmt::Display for pb::MetricType {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        match *self {
            pb::MetricType::Counter => f.pad("counter"),
            pb::MetricType::Histogram => f.pad("histogram"),
            pb::MetricType::Gauge => f.pad("gauge"),
            pb::MetricType::Summary => f.pad("summary"),
            pb::MetricType::Untyped => f.pad("untyped"),
        }
    }
}

// ===== impl LabelPair =====

impl fmt::Display for pb::LabelPair {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        if let pb::LabelPair {
            name: Some(ref name),
            value: Some(ref value),
        } = *self {
            write!(f, "{}=\"{}\"", name, value)
        } else {
            Ok(())
        }
    }
}


/// Format a list of labels in the Prometheus text export format.
#[derive(Clone, Debug)]
struct DisplayLabels<'a>(&'a [pb::LabelPair]);

impl<'a> fmt::Display for DisplayLabels<'a> {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        let mut labels = self.0.into_iter();

        if let Some(label) = labels.next() {
            // format the first label pair without a comma.
            write!(f, "{}", label)?;

            // format the remaining pairs with a comma preceeding them.
            for label in labels {
                write!(f, ",{}", label)?;
            }
        }

        Ok(())
    }
}

#[derive(Copy, Clone, Debug)]
struct DisplayFloat(f64);

impl fmt::Display for DisplayFloat {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        match self.0 {
            v if v == f64::INFINITY => f.pad("+Inf"),
            v if v == f64::NEG_INFINITY => f.pad("-Inf"),
            v if v.is_nan() => f.pad("Nan"),
            v => write!(f, "{}", v),
        }
    }
}



#[cfg(test)]
mod tests {

}
