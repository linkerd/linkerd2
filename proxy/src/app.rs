use config::{self, Config};
use logging;

pub fn init() -> Result<Config, config::Error> {
    logging::init();
    Config::load_from_env()
}

