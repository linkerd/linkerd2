import _ from 'lodash';
import Percentage from './Percentage.js';
import PropTypes from 'prop-types';

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

const getTlsRequestPercentage = row => {
  if (_.isEmpty(row.stats)) {
    return null;
  }
  let tlsRequests = parseInt(_.get(row, ["stats", "tlsRequestCount"], 0), 10);
  return new Percentage(tlsRequests, getTotalRequests(row));
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

const processStatTable = table => {
  return _(table.podGroup.rows).map(row => {
    return {
      name: row.resource.name,
      namespace: row.resource.namespace,
      type: row.resource.type,
      totalRequests: getTotalRequests(row),
      requestRate: getRequestRate(row),
      successRate: getSuccessRate(row),
      latency: getLatency(row),
      tlsRequestPercent: getTlsRequestPercentage(row),
      added: row.meshedPodCount === row.runningPodCount,
      pods: {
        totalPods: row.runningPodCount,
        meshedPods: row.meshedPodCount,
        meshedPercentage: new Percentage(parseInt(row.meshedPodCount, 10), parseInt(row.runningPodCount, 10))
      },
      errors: row.errorsByPod
    };
  })
    .compact()
    .sortBy("name")
    .value();
};

export const processSingleResourceRollup = rawMetrics => {
  let result = processMultiResourceRollup(rawMetrics);
  if (_.size(result) > 1) {
    console.error("Multi metric returned; expected single metric.");
  }
  if (_.isEmpty(result)) {
    return [];
  }
  return _.values(result)[0];
};

export const processMultiResourceRollup = rawMetrics => {
  if (_.isEmpty(rawMetrics.ok) || _.isEmpty(rawMetrics.ok.statTables)) {
    return {};
  }

  let metricsByResource = {};
  _.each(rawMetrics.ok.statTables, table => {
    if (_.isEmpty(table.podGroup.rows)) {
      return;
    }

    // assumes all rows in a podGroup have the same resource type
    let resource = _.get(table, ["podGroup", "rows", 0, "resource", "type"]);

    metricsByResource[resource] = processStatTable(table);
  });
  return metricsByResource;
};

export const excludeResourcesFromRollup = (rollupMetrics, resourceTypeList) => {
  for (let i = 0; i < resourceTypeList.length; i++) {
    delete rollupMetrics[resourceTypeList[i]];
  }
  return rollupMetrics;
};

export const metricsPropType = PropTypes.shape({
  ok: PropTypes.shape({
    statTables: PropTypes.arrayOf(PropTypes.shape({
      podGroup: PropTypes.shape({
        rows: PropTypes.arrayOf(PropTypes.shape({
          failedPodCount: PropTypes.string,
          meshedPodCount: PropTypes.string.isRequired,
          resource: PropTypes.shape({
            name: PropTypes.string.isRequired,
            namespace: PropTypes.string.isRequired,
            type: PropTypes.string.isRequired,
          }).isRequired,
          runningPodCount: PropTypes.string.isRequired,
          stats: PropTypes.shape({
            failureCount: PropTypes.string.isRequired,
            latencyMsP50: PropTypes.string.isRequired,
            latencyMsP95: PropTypes.string.isRequired,
            latencyMsP99: PropTypes.string.isRequired,
            tlsRequestCount: PropTypes.string.isRequired,
            successCount: PropTypes.string.isRequired,
          }),
          timeWindow: PropTypes.string.isRequired,
        }).isRequired),
      }),
    }).isRequired).isRequired,
  }),
});

export const processedMetricsPropType = PropTypes.shape({
  name: PropTypes.string.isRequired,
  namespace: PropTypes.string.isRequired,
  totalRequests: PropTypes.number.isRequired,
  requestRate: PropTypes.number,
  successRate: PropTypes.number,
});
