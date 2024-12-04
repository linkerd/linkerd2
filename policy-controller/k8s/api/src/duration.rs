use serde::{de, Deserialize, Deserializer, Serialize, Serializer};
use std::{fmt, str::FromStr, time::Duration};

#[derive(Copy, Clone, PartialEq, Eq)]
pub struct K8sDuration {
    duration: Duration,
    is_negative: bool,
}

#[derive(Debug, thiserror::Error, Eq, PartialEq)]
#[non_exhaustive]
pub enum ParseError {
    #[error("invalid unit: {}", EXPECTED_UNITS)]
    InvalidUnit,

    #[error("missing a unit: {}", EXPECTED_UNITS)]
    NoUnit,

    #[error("invalid floating-point number: {}", .0)]
    NotANumber(#[from] std::num::ParseFloatError),
}

const EXPECTED_UNITS: &str = "expected one of 'ns', 'us', '\u{00b5}s', 'ms', 's', 'm', or 'h'";

impl From<Duration> for K8sDuration {
    fn from(duration: Duration) -> Self {
        Self {
            duration,
            is_negative: false,
        }
    }
}

impl From<K8sDuration> for Duration {
    fn from(K8sDuration { duration, .. }: K8sDuration) -> Self {
        duration
    }
}

impl K8sDuration {
    #[inline]
    #[must_use]
    pub fn is_negative(&self) -> bool {
        self.is_negative
    }
}

impl fmt::Debug for K8sDuration {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        use std::fmt::Write;
        if self.is_negative {
            f.write_char('-')?;
        }
        fmt::Debug::fmt(&self.duration, f)
    }
}

impl fmt::Display for K8sDuration {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        use std::fmt::Write;
        if self.is_negative {
            f.write_char('-')?;
        }
        fmt::Debug::fmt(&self.duration, f)
    }
}

impl FromStr for K8sDuration {
    type Err = ParseError;

    fn from_str(mut s: &str) -> Result<Self, Self::Err> {
        // implements the same format as
        // https://cs.opensource.google/go/go/+/refs/tags/go1.20.4:src/time/format.go;l=1589

        fn duration_from_units(val: f64, unit: &str) -> Result<Duration, ParseError> {
            const MINUTE: Duration = Duration::from_secs(60);
            // https://cs.opensource.google/go/go/+/refs/tags/go1.20.4:src/time/format.go;l=1573
            let base = match unit {
                "ns" => Duration::from_nanos(1),
                // U+00B5 is the "micro sign" while U+03BC is "Greek letter mu"
                "us" | "\u{00b5}s" | "\u{03bc}s" => Duration::from_micros(1),
                "ms" => Duration::from_millis(1),
                "s" => Duration::from_secs(1),
                "m" => MINUTE,
                "h" => MINUTE * 60,
                _ => return Err(ParseError::InvalidUnit),
            };
            Ok(base.mul_f64(val))
        }

        // Go durations are signed. Rust durations aren't. So we need to ignore
        // this for now.
        let is_negative = s.starts_with('-');
        s = s.trim_start_matches('+').trim_start_matches('-');

        let mut total = Duration::from_secs(0);
        while !s.is_empty() {
            if let Some(unit_start) = s.find(|c: char| c.is_alphabetic()) {
                let (val, rest) = s.split_at(unit_start);
                let val = val.parse::<f64>()?;
                let unit = if let Some(next_numeric_start) = rest.find(|c: char| !c.is_alphabetic())
                {
                    let (unit, rest) = rest.split_at(next_numeric_start);
                    s = rest;
                    unit
                } else {
                    s = "";
                    rest
                };
                total += duration_from_units(val, unit)?;
            } else if s == "0" {
                return Ok(K8sDuration {
                    duration: Duration::from_secs(0),
                    is_negative,
                });
            } else {
                return Err(ParseError::NoUnit);
            }
        }

        Ok(K8sDuration {
            duration: total,
            is_negative,
        })
    }
}

impl Serialize for K8sDuration {
    fn serialize<S>(&self, serializer: S) -> Result<S::Ok, S::Error>
    where
        S: Serializer,
    {
        serializer.collect_str(self)
    }
}

impl<'de> Deserialize<'de> for K8sDuration {
    fn deserialize<D>(deserializer: D) -> Result<Self, D::Error>
    where
        D: Deserializer<'de>,
    {
        struct Visitor;
        impl de::Visitor<'_> for Visitor {
            type Value = K8sDuration;

            fn expecting(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
                f.write_str("a string in Go `time.Duration.String()` format")
            }

            fn visit_str<E>(self, value: &str) -> Result<Self::Value, E>
            where
                E: de::Error,
            {
                let val = value.parse::<K8sDuration>().map_err(de::Error::custom)?;
                Ok(val)
            }
        }
        deserializer.deserialize_str(Visitor)
    }
}

impl schemars::JsonSchema for K8sDuration {
    // see
    // https://github.com/kubernetes/apimachinery/blob/756e2227bf3a486098f504af1a0ffb736ad16f4c/pkg/apis/meta/v1/duration.go#L61
    fn schema_name() -> String {
        "K8sDuration".to_owned()
    }

    fn is_referenceable() -> bool {
        false
    }

    fn json_schema(_: &mut schemars::gen::SchemaGenerator) -> schemars::schema::Schema {
        schemars::schema::SchemaObject {
            instance_type: Some(schemars::schema::InstanceType::String.into()),
            // the format should *not* be "duration", because "duration" means
            // the duration is formatted in ISO 8601, as described here:
            // https://datatracker.ietf.org/doc/html/draft-handrews-json-schema-validation-02#section-7.3.1
            format: None,
            ..Default::default()
        }
        .into()
    }
}
#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_the_same_as_go() {
        const MINUTE: Duration = Duration::from_secs(60);
        const HOUR: Duration = Duration::from_secs(60 * 60);
        // from Go:
        // https://cs.opensource.google/go/go/+/refs/tags/go1.20.4:src/time/time_test.go;l=891-951
        // ```
        // var parseDurationTests = []struct {
        // 	in   string
        // 	want Duration
        // }{
        let cases: &[(&str, K8sDuration)] = &[
            // 	// simple
            // 	{"0", 0},
            ("0", Duration::from_secs(0).into()),
            // 	{"5s", 5 * Second},
            ("5s", Duration::from_secs(5).into()),
            // 	{"30s", 30 * Second},
            ("30s", Duration::from_secs(30).into()),
            // 	{"1478s", 1478 * Second},
            ("1478s", Duration::from_secs(1478).into()),
            // 	// sign
            // 	{"-5s", -5 * Second},
            (
                "-5s",
                K8sDuration {
                    duration: Duration::from_secs(5),
                    is_negative: true,
                },
            ),
            // 	{"+5s", 5 * Second},
            ("+5s", Duration::from_secs(5).into()),
            // 	{"-0", 0},
            (
                "-0",
                K8sDuration {
                    duration: Duration::from_secs(0),
                    is_negative: true,
                },
            ),
            // 	{"+0", 0},
            ("+0", Duration::from_secs(0).into()),
            // 	// decimal
            // 	{"5.0s", 5 * Second},
            ("5s", Duration::from_secs(5).into()),
            // 	{"5.6s", 5*Second + 600*Millisecond},
            (
                "5.6s",
                (Duration::from_secs(5) + Duration::from_millis(600)).into(),
            ),
            // 	{"5.s", 5 * Second},
            ("5.s", Duration::from_secs(5).into()),
            // 	{".5s", 500 * Millisecond},
            (".5s", Duration::from_millis(500).into()),
            // 	{"1.0s", 1 * Second},
            ("1.0s", Duration::from_secs(1).into()),
            // 	{"1.00s", 1 * Second},
            ("1.00s", Duration::from_secs(1).into()),
            // 	{"1.004s", 1*Second + 4*Millisecond},
            (
                "1.004s",
                (Duration::from_secs(1) + Duration::from_millis(4)).into(),
            ),
            // 	{"1.0040s", 1*Second + 4*Millisecond},
            (
                "1.0040s",
                (Duration::from_secs(1) + Duration::from_millis(4)).into(),
            ),
            // 	{"100.00100s", 100*Second + 1*Millisecond},
            (
                "100.00100s",
                (Duration::from_secs(100) + Duration::from_millis(1)).into(),
            ),
            // 	// different units
            // 	{"10ns", 10 * Nanosecond},
            ("10ns", Duration::from_nanos(10).into()),
            // 	{"11us", 11 * Microsecond},
            ("11us", Duration::from_micros(11).into()),
            // 	{"12µs", 12 * Microsecond}, // U+00B5
            ("12µs", Duration::from_micros(12).into()),
            // 	{"12μs", 12 * Microsecond}, // U+03BC
            ("12μs", Duration::from_micros(12).into()),
            // 	{"13ms", 13 * Millisecond},
            ("13ms", Duration::from_millis(13).into()),
            // 	{"14s", 14 * Second},
            ("14s", Duration::from_secs(14).into()),
            // 	{"15m", 15 * Minute},
            ("15m", (15 * MINUTE).into()),
            // 	{"16h", 16 * Hour},
            ("16h", (16 * HOUR).into()),
            // 	// composite durations
            // 	{"3h30m", 3*Hour + 30*Minute},
            ("3h30m", (3 * HOUR + 30 * MINUTE).into()),
            // 	{"10.5s4m", 4*Minute + 10*Second + 500*Millisecond},
            (
                "10.5s4m",
                (4 * MINUTE + Duration::from_secs(10) + Duration::from_millis(500)).into(),
            ),
            // 	{"-2m3.4s", -(2*Minute + 3*Second + 400*Millisecond)},
            (
                "-2m3.4s",
                K8sDuration {
                    duration: 2 * MINUTE + Duration::from_secs(3) + Duration::from_millis(400),
                    is_negative: true,
                },
            ),
            // 	{"1h2m3s4ms5us6ns", 1*Hour + 2*Minute + 3*Second + 4*Millisecond + 5*Microsecond + 6*Nanosecond},
            (
                "1h2m3s4ms5us6ns",
                (1 * HOUR
                    + 2 * MINUTE
                    + Duration::from_secs(3)
                    + Duration::from_millis(4)
                    + Duration::from_micros(5)
                    + Duration::from_nanos(6))
                .into(),
            ),
            // 	{"39h9m14.425s", 39*Hour + 9*Minute + 14*Second + 425*Millisecond},
            (
                "39h9m14.425s",
                (39 * HOUR + 9 * MINUTE + Duration::from_secs(14) + Duration::from_millis(425))
                    .into(),
            ),
            // 	// large value
            // 	{"52763797000ns", 52763797000 * Nanosecond},
            ("52763797000ns", Duration::from_nanos(52763797000).into()),
            // 	// more than 9 digits after decimal point, see https://golang.org/issue/6617
            // 	{"0.3333333333333333333h", 20 * Minute},
            ("0.3333333333333333333h", (20 * MINUTE).into()),
            // 	// 9007199254740993 = 1<<53+1 cannot be stored precisely in a float64
            // 	{"9007199254740993ns", (1<<53 + 1) * Nanosecond},
            (
                "9007199254740993ns",
                Duration::from_nanos((1 << 53) + 1).into(),
            ),
            // Rust Durations can handle larger durations than Go's
            // representation, so skip these tests for their precision limits

            // 	// largest duration that can be represented by int64 in nanoseconds
            // 	{"9223372036854775807ns", (1<<63 - 1) * Nanosecond},
            // ("9223372036854775807ns", Duration::from_nanos((1 << 63) - 1).into()),
            // 	{"9223372036854775.807us", (1<<63 - 1) * Nanosecond},
            // ("9223372036854775.807us", Duration::from_nanos((1 << 63) - 1).into()),
            // 	{"9223372036s854ms775us807ns", (1<<63 - 1) * Nanosecond},
            // 	{"-9223372036854775808ns", -1 << 63 * Nanosecond},
            // 	{"-9223372036854775.808us", -1 << 63 * Nanosecond},
            // 	{"-9223372036s854ms775us808ns", -1 << 63 * Nanosecond},
            // 	// largest negative value
            // 	{"-9223372036854775808ns", -1 << 63 * Nanosecond},
            // 	// largest negative round trip value, see https://golang.org/issue/48629
            // 	{"-2562047h47m16.854775808s", -1 << 63 * Nanosecond},

            // 	// huge string; issue 15011.
            // 	{"0.100000000000000000000h", 6 * Minute},
            ("0.100000000000000000000h", (6 * MINUTE).into()), // 	// This value tests the first overflow check in leadingFraction.
                                                               // 	{"0.830103483285477580700h", 49*Minute + 48*Second + 372539827*Nanosecond},
                                                               // }
                                                               // ```
        ];

        for (input, expected) in cases {
            let parsed = dbg!(input).parse::<K8sDuration>().unwrap();
            assert_eq!(&dbg!(parsed), expected);
        }
    }
}
