import _ from 'lodash';
import Percentage from './Percentage';

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

const getTotalRequests = row => {
  let success = parseInt(_.get(row, ["stats", "successCount"], 0), 10);
  let failure = parseInt(_.get(row, ["stats", "failureCount"], 0), 10);

  return success + failure;
};

const getRequestRate = row => {
  if (_.isEmpty(row.stats)) {
    return null;
  }

  let seconds = 0;

  if (row.timeWindow === "10s") { seconds = 10; }
  if (row.timeWindow === "1m") { seconds = 60; }
  if (row.timeWindow === "10m") { seconds = 600; }
  if (row.timeWindow === "1h") { seconds = 3600; }

  if (seconds === 0) {
    return null;
  } else {
    return getTotalRequests(row) / seconds;
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

const getSecuredTrafficPercentage = row => {
  if (_.isEmpty(row.stats)) {
    return null;
  }
  let meshedRequests = parseInt(_.get(row, ["stats", "securedRequestCount"], 0), 10);
  return new Percentage(meshedRequests, getTotalRequests(row));
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

const processStatTable = (table, controllerNamespace, includeConduit) => {
  return _(table.podGroup.rows).map(row => {
    if (row.resource.namespace === kubernetesNs) {
      return null;
    }
    if (row.resource.namespace === controllerNamespace && !includeConduit) {
      return null;
    }

    return {
      name: row.resource.name,
      namespace: row.resource.namespace,
      totalRequests: getTotalRequests(row),
      requestRate: getRequestRate(row),
      successRate: getSuccessRate(row),
      latency: getLatency(row),
      securedRequestPercent: getSecuredTrafficPercentage(row),
      added: row.meshedPodCount === row.runningPodCount
    };
  })
    .compact()
    .sortBy("name")
    .value();
};

export const processSingleResourceRollup = (rawMetrics, controllerNamespace, includeConduit) => {
  let result = processMultiResourceRollup(rawMetrics, controllerNamespace, includeConduit);
  if (_.size(result) > 1) {
    console.error("Multi metric returned; expected single metric.");
  }
  return _.values(result)[0];
};

export const processMultiResourceRollup = (rawMetrics, controllerNamespace, includeConduit) => {
  if (_.isEmpty(rawMetrics.ok) || _.isEmpty(rawMetrics.ok.statTables)) {
    return {};
  }

  if (_.isEmpty(controllerNamespace)) {
    controllerNamespace = defaultControllerNs;
  }

  let metricsByResource = {};
  _.each(rawMetrics.ok.statTables, table => {
    if (_.isEmpty(table.podGroup.rows)) {
      return;
    }

    // assumes all rows in a podGroup have the same resource type
    let resource = _.get(table, ["podGroup", "rows", 0, "resource", "type"]);

    metricsByResource[resource] = processStatTable(table, controllerNamespace, includeConduit);
  });
  return metricsByResource;
};
