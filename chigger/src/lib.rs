mod destination;

pub use destination::DestinationClient;

pub fn random_suffix(len: usize) -> String {
    use rand::Rng;

    struct LowercaseAlphanumeric;

    // Modified from `rand::distributions::Alphanumeric`
    //
    // Copyright 2018 Developers of the Rand project
    // Copyright (c) 2014 The Rust Project Developers
    //
    // Licensed under the Apache License, Version 2.0 (the "License");
    // you may not use this file except in compliance with the License.
    // You may obtain a copy of the License at
    //
    //     http://www.apache.org/licenses/LICENSE-2.0
    //
    // Unless required by applicable law or agreed to in writing, software
    // distributed under the License is distributed on an "AS IS" BASIS,
    // WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
    // See the License for the specific language governing permissions and
    // limitations under the License.
    impl rand::distributions::Distribution<u8> for LowercaseAlphanumeric {
        fn sample<R: rand::Rng + ?Sized>(&self, rng: &mut R) -> u8 {
            const RANGE: u32 = 26 + 10;
            const CHARSET: &[u8] = b"abcdefghijklmnopqrstuvwxyz0123456789";
            loop {
                let var = rng.next_u32() >> (32 - 6);
                if var < RANGE {
                    return CHARSET[var as usize];
                }
            }
        }
    }

    rand::thread_rng()
        .sample_iter(&LowercaseAlphanumeric)
        .take(len)
        .map(char::from)
        .collect()
}
