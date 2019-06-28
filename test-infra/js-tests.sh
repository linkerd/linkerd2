#!/bin/bash
#
# Entrypoint for JS tests.

set -eux

curl -o- -L https://yarnpkg.com/install.sh | bash -s -- --version 1.7.0

export PATH="$HOME/.yarn/bin:$PATH"
export NODE_ENV=test

./bin/web
./bin/web test --reporters=jest-dot-reporter
