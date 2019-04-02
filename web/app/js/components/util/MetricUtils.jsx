import Percentage from './Percentage.js';
import PropTypes from 'prop-types';
import _compact from 'lodash/compact';
import _each from 'lodash/each';
import _get from 'lodash/get';
import _isEmpty from 'lodash/isEmpty';
import _isNull from 'lodash/isNull';
import _map from 'lodash/map';
import _orderBy from 'lodash/orderBy';
import _reduce from 'lodash/reduce';
import _size from 'lodash/size';
import _values from 'lodash/values';

export const getSuccessRateClassification = (rate, successRateLabels = srArcClassLabels) => {
  if (_isNull(rate)) {
    return successRateLabels.default;
  }

  if (rate < 0.9) {
    return successRateLabels.poor;
  } else if (rate < 0.95) {
    return successRateLabels.warning;
  } else {
    return successRateLabels.good;
  }
};

const srArcClassLabels = {
  good: "good",
  warning: "warning",
  poor: "poor",
  default: "default"
};

const getTotalRequests = row => {
  let success = parseInt(_get(row, ["stats", "successCount"], 0), 10);
  let failure = parseInt(_get(row, ["stats", "failureCount"], 0), 10);

  return success + failure;
};

const timeWindowSeconds = timeWindow => {
  let seconds = 0;

  if (timeWindow === "10s") { seconds = 10; }
  if (timeWindow === "1m") { seconds = 60; }
  if (timeWindow === "10m") { seconds = 600; }
  if (timeWindow === "1h") { seconds = 3600; }

  return seconds;
};

const getRequestRate = row => {
  if (_isEmpty(row.stats)) {
    return null;
  }

  let seconds = timeWindowSeconds(row.timeWindow);

  if (seconds === 0) {
    return null;
  } else {
    return getTotalRequests(row) / seconds;
  }
};

const getSuccessRate = row => {
  if (_isEmpty(row.stats)) {
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
  if (_isEmpty(row.stats)) {
    return {};
  } else {
    return {
      P50: parseInt(row.stats.latencyMsP50, 10),
      P95: parseInt(row.stats.latencyMsP95, 10),
      P99: parseInt(row.stats.latencyMsP99, 10),
    };
  }
};

const getTcpStats = row => {
  if (_isEmpty(row.tcpStats)) {
    return {};
  } else {
    let seconds = timeWindowSeconds(row.timeWindow);
    let readBytes = parseInt(row.tcpStats.readBytesTotal, 0);
    let writeBytes = parseInt(row.tcpStats.writeBytesTotal, 0);
    return {
      openConnections: parseInt(row.tcpStats.openConnections, 0),
      readBytes,
      writeBytes,
      readRate: seconds === 0 ? null : readBytes / seconds,
      writeRate: seconds === 0 ? null : writeBytes / seconds,
    };
  }
};

const processStatTable = table => {
  let rows = _compact(table.podGroup.rows.map(row => {
    let runningPodCount = parseInt(row.runningPodCount, 10);
    let meshedPodCount = parseInt(row.meshedPodCount, 10);
    return {
      key: `${row.resource.namespace}-${row.resource.type}-${row.resource.name}`,
      name: row.resource.name,
      namespace: row.resource.namespace,
      type: row.resource.type,
      totalRequests: getTotalRequests(row),
      requestRate: getRequestRate(row),
      successRate: getSuccessRate(row),
      latency: getLatency(row),
      tcp: getTcpStats(row),
      added: runningPodCount > 0 && meshedPodCount > 0,
      pods: {
        totalPods: row.runningPodCount,
        meshedPods: row.meshedPodCount,
        meshedPercentage: new Percentage(meshedPodCount, runningPodCount)
      },
      errors: row.errorsByPod
    };
  }));

  return _orderBy(rows, r => r.name);
};

export const DefaultRoute = "[DEFAULT]";
export const processTopRoutesResults = rows => {
  return _map(rows, row => ({
    route: row.route || DefaultRoute,
    tooltip: !_isEmpty(row.route) ? null : "Traffic does not match any configured routes",
    authority: row.authority,
    totalRequests: getTotalRequests(row),
    requestRate: getRequestRate(row),
    successRate: getSuccessRate(row),
    latency: getLatency(row),
  }
  ));
};

export const processSingleResourceRollup = rawMetrics => {
  let result = processMultiResourceRollup(rawMetrics);
  if (_size(result) > 1) {
    console.error("Multi metric returned; expected single metric.");
  }
  if (_isEmpty(result)) {
    return [];
  }
  return _values(result)[0];
};

export const processMultiResourceRollup = rawMetrics => {
  if (_isEmpty(rawMetrics.ok) || _isEmpty(rawMetrics.ok.statTables)) {
    return {};
  }

  let metricsByResource = {};
  _each(rawMetrics.ok.statTables, table => {
    if (_isEmpty(table.podGroup.rows)) {
      return;
    }

    // assumes all rows in a podGroup have the same resource type
    let resource = _get(table, ["podGroup", "rows", 0, "resource", "type"]);

    metricsByResource[resource] = processStatTable(table);
  });
  return metricsByResource;
};

export const groupResourcesByNs = apiRsp => {
  let statTables = _get(apiRsp, ["ok", "statTables"]);
  let authoritiesByNs = {};
  let resourcesByNs = _reduce(statTables, (mem, table) => {
    _each(table.podGroup.rows, row => {
      // filter out resources that aren't meshed. note that authorities don't
      // have pod counts and therefore can't be filtered out here
      if (row.meshedPodCount === "0" && row.resource.type !== "authority") {
        return;
      }

      if (!mem[row.resource.namespace]) {
        mem[row.resource.namespace] = [];
        authoritiesByNs[row.resource.namespace] = [];
      }

      switch (row.resource.type.toLowerCase()) {
        case "service":
          break;
        case "authority":
          authoritiesByNs[row.resource.namespace].push(row.resource.name);
          break;
        default:
          mem[row.resource.namespace].push(`${row.resource.type}/${row.resource.name}`);
      }
    });
    return mem;
  }, {});
  return {
    authoritiesByNs,
    resourcesByNs
  };
};

export const excludeResourcesFromRollup = (rollupMetrics, resourcesToExclude) => {
  _each(resourcesToExclude, resource => {
    delete rollupMetrics[resource];
  });
  return rollupMetrics;
};

export const emptyMetric = {
  key: "",
  name: "",
  namespace: "",
  type: "",
  totalRequests: null,
  requestRate: null,
  successRate: null,
  latency: null,
  added: false,
  pods: {
    totalPods: null,
    meshedPods: null,
    meshedPercentage: null
  }
};

export const metricsPropType = PropTypes.shape({
  ok: PropTypes.shape({
    statTables: PropTypes.arrayOf(PropTypes.shape({
      podGroup: PropTypes.shape({
        rows: PropTypes.arrayOf(PropTypes.shape({
          failedPodCount: PropTypes.string,
          meshedPodCount: PropTypes.string,
          resource: PropTypes.shape({
            name: PropTypes.string,
            namespace: PropTypes.string,
            type: PropTypes.string,
          }).isRequired,
          runningPodCount: PropTypes.string,
          stats: PropTypes.shape({
            failureCount: PropTypes.string,
            latencyMsP50: PropTypes.string,
            latencyMsP95: PropTypes.string,
            latencyMsP99: PropTypes.string,
            successCount: PropTypes.string,
          }),
          timeWindow: PropTypes.string,
        }).isRequired),
      }),
    }).isRequired).isRequired,
  }),
});

export const processedMetricsPropType = PropTypes.shape({
  name: PropTypes.string.isRequired,
  namespace: PropTypes.string.isRequired,
  totalRequests: PropTypes.number,
  requestRate: PropTypes.number,
  successRate: PropTypes.number,
});
