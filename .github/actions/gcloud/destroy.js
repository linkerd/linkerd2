const configure = require('./configure.js');
const core = require('@actions/core');
const exec = require('@actions/exec');

async function destroy() {
  try {
    const name = core.getInput('name');
    await exec.exec('gcloud container clusters delete --quiet', [name]);
  } catch (e) {
    core.setFailed(e.message)
  }
}

async function run() {
  try {
    if (core.getInput('create')) {
      await configure.gcloud();
      await destroy();
    }
  } catch (e) {
    core.setFailed(e.message)
  }
}

try {
  run();
} catch (e) {
  core.setFailed(e.message);
}

