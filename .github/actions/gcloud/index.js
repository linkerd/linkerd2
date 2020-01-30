const core = require('@actions/core');
const exec = require('@actions/exec');
const fs = require('fs');

async function getClusterName() {
  let tag, clientVersion;
  await exec.exec('bin/root-tag', [], {
      env: {
          CI_FORCE_CLEAN: 1
      },
      listeners: {
          stdout: (data) => {
              tag = data.toString().trim()
          }
      }
  });

  await exec.exec(`${process.env.HOME}/.linkerd version --client --short`, [], {
      listeners: {
          stdout: (data) => {
              clientVersion = data.toString().trim()
          }
      }
  });

  // validate CLI version matches the repo
  if (tag != clientVersion) {
      throw `tag ${tag} differs from clientversion ${clientVersion}`
  }
  console.log('Linkerd CLI version:', tag)

  // Last part is to distinguish runs on the same sha (run-id is unique per CI run).
  // run-id has to be provided as an input because it turns out it's not available
  // through github.context.run_id
  const name = `testing-${tag}-${core.getInput('run-id')}`;
  console.log('Cluster name:', name);
  return name;
}

async function configure() {
  try {
    await exec.exec('gcloud auth activate-service-account',
      ['--key-file',  `${process.env.HOME}/.gcp.json`]);
    await exec.exec('gcloud config set core/project', [core.getInput('gcp_project')]);
    await exec.exec('gcloud config set compute/zone', [core.getInput('gcp_zone')]);
    await exec.exec('gcloud auth configure-docker --quiet');

    if (core.getInput('create') || core.getInput('destroy')) {
      const name = await getClusterName();
      if (core.getInput('create')) {
        await exec.exec('gcloud container clusters create',
          [name, '--cluster-version', '1.15.7-gke.23', '--num-nodes', 1, '--machine-type',
          'n1-standard-2', '--enable-network-policy']);
        await exec.exec('gcloud config set container/cluster',  [name]);
        await exec.exec('gcloud container clusters get-credentials', [name]);

        let sa;
        await exec.exec('gcloud config get-value account', [], {
          listeners: {
            stdout: (data) => {
              sa = data.toString()
            }
          }
        });
        await exec.exec('kubectl create clusterrolebinding ci-cluster-admin --clusterrole=cluster-admin',
          ['--user', sa]);
      } else {
        await exec.exec('gcloud container clusters delete --quiet', [name]);
      }
    }
  } catch (e) {
    core.setFailed(e.message)
  }
}

try {
    fs.writeFileSync(process.env.HOME + '/.gcp.json', core.getInput('cloud_sdk_service_account_key'));
    configure()
} catch (e) {
    core.setFailed(e.message);
}
