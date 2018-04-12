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
  let requests = 0;
  let seconds = 0;

  if (!_.isEmpty(row.stats)) {
    requests += parseInt(row.stats.successCount, 10);
    requests += parseInt(row.stats.failureCount, 10);
  }

  if (row.timeWindow === "TEN_SEC") { seconds = 10; }
  if (row.timeWindow === "ONE_MIN") { seconds = 60; }
  if (row.timeWindow === "TEN_MIN") { seconds = 600; }
  if (row.timeWindow === "ONE_HOUR") { seconds = 3600; }

  if (seconds === 0) {
    return 0.0;
  } else {
    return requests / seconds;
  }
};

const getSuccessRate = row => {
  let success = 0;
  let requests = 0;

  if (!_.isEmpty(row.stats)) {
    success += parseInt(row.stats.successCount, 10);
    requests += parseInt(row.stats.successCount, 10);
    requests += parseInt(row.stats.failureCount, 10);
  }

  if (requests === 0) {
    return 0.0;
  } else {
    return success / requests;
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

export const processRollupMetrics = rawMetrics => {
  if (_.isEmpty(rawMetrics.ok) || _.isEmpty(rawMetrics.ok.statTables)) {
    return [];
  }

  let metrics = _.flatMap(rawMetrics.ok.statTables, table => {
    return _.map(table.podGroup.rows, row => {
      return {
        name: row.resource.namespace + "/" + row.resource.name,
        requestRate: getRequestRate(row),
        successRate: getSuccessRate(row),
        latency: getLatency(row)
      };
    });
  });

  return _.compact(_.sortBy(metrics, "name"));
};
