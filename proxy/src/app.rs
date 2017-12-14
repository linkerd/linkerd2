use config::{self, Config, Env};
use convert::TryFrom;
use logging;

pub fn init() -> Result<Config, config::Error> {
    logging::init();
    let config_strings = Env;
    Config::try_from(&config_strings)
}
