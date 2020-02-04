const core = require('@actions/core');
const exec = require('@actions/exec');
const fs = require('fs');

export async function gcloud() {
  try {
    fs.writeFileSync(process.env.HOME + '/.gcp.json', core.getInput('cloud_sdk_service_account_key'));
    await exec.exec('gcloud auth activate-service-account',
      ['--key-file',  `${process.env.HOME}/.gcp.json`]);
    await exec.exec('gcloud config set core/project', [core.getInput('gcp_project')]);
    await exec.exec('gcloud config set compute/zone', [core.getInput('gcp_zone')]);
    await exec.exec('gcloud auth configure-docker --quiet');
  } catch (e) {
    core.setFailed(e.message)
  }
}

