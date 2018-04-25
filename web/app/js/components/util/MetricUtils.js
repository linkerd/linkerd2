import _ from 'lodash';

const getPodCategorization = pod => {
  if (pod.added && pod.status === "Running") {
    return "good";
  } else if (pod.status === "Pending" || pod.status === "Running") {
    return "neutral";
  } else if (pod.status === "Failed") {
    return "bad";
  }
  return ""; // Terminating | Succeeded | Unknown
};

const getRequestRate = row => {
  if (_.isEmpty(row.stats)) {
    return null;
  }

  let success = parseInt(row.stats.successCount, 10);
  let failure = parseInt(row.stats.failureCount, 10);
  let seconds = 0;

  if (row.timeWindow === "10s") { seconds = 10; }
  if (row.timeWindow === "1m") { seconds = 60; }
  if (row.timeWindow === "10m") { seconds = 600; }
  if (row.timeWindow === "1h") { seconds = 3600; }

  if (seconds === 0) {
    return null;
  } else {
    return (success + failure) / seconds;
  }
};

const getSuccessRate = row => {
  if (_.isEmpty(row.stats)) {
    return null;
  }

  let success = parseInt(row.stats.successCount, 10);
  let failure = parseInt(row.stats.failureCount, 10);

  if (success + failure === 0) {
    return null;
  } else {
    return success / (success + failure);
  }
};

const getLatency = row => {
  if (_.isEmpty(row.stats)) {
    return {};
  } else {
    return {
      P50: parseInt(row.stats.latencyMsP50, 10),
      P95: parseInt(row.stats.latencyMsP95, 10),
      P99: parseInt(row.stats.latencyMsP99, 10),
    };
  }
};

export const getPodsByDeployment = pods => {
  return _(pods)
    .reject(p => _.isEmpty(p.deployment) || p.controlPlane)
    .groupBy('deployment')
    .map((componentPods, name) => {
      let podsWithStatus = _.chain(componentPods)
        .map(p => {
          return _.merge({}, p, { value: getPodCategorization(p) });
        })
        .reject(p => _.isEmpty(p.value))
        .value();

      return {
        name: name,
        added: _.every(componentPods, 'added'),
        pods: _.sortBy(podsWithStatus, 'name')
      };
    })
    .reject(p => _.isEmpty(p.pods))
    .sortBy('name')
    .value();
};

export const getComponentPods = componentPods => {
  return _.chain(componentPods)
    .map( p => {
      return { name: p.name, value: getPodCategorization(p) };
    })
    .reject(p => _.isEmpty(p.value))
    .sortBy("name")
    .value();
};

const kubernetesNs = "kube-system";
const defaultControllerNs = "conduit";
export const processRollupMetrics = (rawMetrics, controllerNamespace) => {
  if (_.isEmpty(rawMetrics.ok) || _.isEmpty(rawMetrics.ok.statTables)) {
    return [];
  }
  if (_.isEmpty(controllerNamespace)) {
    controllerNamespace = defaultControllerNs;
  }
  let metrics = _.flatMap(rawMetrics.ok.statTables, table => {
    return _.map(table.podGroup.rows, row => {
      if (row.resource.namespace === kubernetesNs || row.resource.namespace === controllerNamespace) {
        return null;
      }
      return {
        name: row.resource.name,
        namespace: row.resource.namespace,
        requestRate: getRequestRate(row),
        successRate: getSuccessRate(row),
        latency: getLatency(row),
        added: row.meshedPodCount === row.totalPodCount
      };
    });
  });

  return _.compact(_.sortBy(metrics, "name"));
};
