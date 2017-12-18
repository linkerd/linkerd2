import _ from 'lodash';

const gaugeAccessor = ["datapoints", 0, "value", "gauge"];
const latencyAccessor = ["datapoints", 0, "value", "histogram", "values"];

const convertTs = rawTs => {
  return _.map(rawTs, metric => {
    return {
      timestamp: metric.timestampMs,
      value: _.get(metric, "value.gauge")
    };
  });
};

const convertLatencyTs = rawTs => {
  let latencies = _.flatMap(rawTs, metric => {
    return _.map(_.get(metric, ["value", "histogram", "values"]), hist => {
      return {
        timestamp: metric.timestampMs,
        value: hist.value,
        label: hist.label
      };
    });
  });
  // this could be made more efficient by combining this with the map
  return _.groupBy(latencies, 'label');
};

export const processTimeseriesMetrics = (rawTs, targetEntity) => {
  let tsbyEntity = _.groupBy(rawTs, "metadata." + targetEntity);
  return _.reduce(tsbyEntity, (mem, metrics, entity) => {
    mem[entity] = mem[entity] || {};
    _.each(metrics, metric => {
      if (metric.name !== "LATENCY") {
        mem[entity][metric.name] = convertTs(metric.datapoints);
      } else {
        mem[entity][metric.name] = convertLatencyTs(metric.datapoints);
      }
    });
    return mem;
  }, {});
};

export const processRollupMetrics = (rawMetrics, targetEntity) => {
  let byEntity = _.groupBy(rawMetrics, "metadata." + targetEntity);
  let metrics = _.map(byEntity, (data, entity) => {
    if (!entity) return;

    let requestRate = 0;
    let successRate = 0;
    let latency = {};

    _.each(data, datum => {
      if (datum.name === "REQUEST_RATE") {
        requestRate = _.round(_.get(datum, gaugeAccessor, 0), 2);
      } else if (datum.name === "SUCCESS_RATE") {
        successRate = _.get(datum, gaugeAccessor);
      } else if (datum.name === "LATENCY") {
        let latencies = _.get(datum, latencyAccessor);
        latency = _.groupBy(latencies, 'label');
      }
    });

    return {
      name: entity,
      requestRate: requestRate,
      successRate: successRate,
      latency: latency,
      added: true
    };
  });

  return _.compact(_.sortBy(metrics, "name"));
};

export const emptyMetric = (name, added) => {
  return {
    name: name,
    requestRate: null,
    successRate: null,
    latency: null,
    added: added
  };
};
