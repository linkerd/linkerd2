import _ from 'lodash';

const counterAccessor = ["datapoints", 0, "value", "counter"];
const gaugeAccessor = ["datapoints", 0, "value", "gauge"];
const latencyAccessor = ["datapoints", 0, "value", "histogram", "values"];

const convertTs = (rawTs) => {
  return _.map(rawTs, metric => {
    return {
      timestamp: metric.timestampMs,
      value: _.get(metric, "value.gauge")
    }
  });
}

const convertLatencyTs = (rawTs) => {
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
}

export const processMetrics = (rawMetrics, rawTs, targetEntity) => {
  let byEntity = _.groupBy(rawMetrics, m => {
    return m.metadata[targetEntity];
  });

  let tsbyEntity = _.groupBy(rawTs, "metadata." + targetEntity);
  let tsMap = _.reduce(tsbyEntity, (mem, metrics, entity) => {
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

    let p99latency = _.get(latency, ["P99", 0, "value"]);

    return {
      name: entity,
      timeseries: {
        requestRate: _.get(tsMap, [entity, "REQUEST_RATE"], []),
        successRate: _.get(tsMap, [entity, "SUCCESS_RATE"], []),
        latency: _.get(tsMap, [entity, "LATENCY"], [])
      },
      rollup: {
        requestRate: requestRate,
        successRate: successRate,
        latency: latency
      },
      scatterplot: {
        label: entity,
        success: successRate,
        latency: p99latency
      },
      latency: latency,
      added: true
    }
  });

  return _.sortBy(metrics, "name");
}

export const emptyMetric = name => {
  return {
    name: name,
    timeseries: {
      requestRate: [],
      successRate: [],
      latency: []
    },
    rollup: {
      requestRate: null,
      successRate: null,
      latency: null
    },
    scatterplot: {
      label: name,
      success: 0,
      latency: 0
    },
    latency: 0,
    added: false
  }
}

export const processTsWithLatencyBreakdown = rawMetrics => {
  return _.reduce(rawMetrics, (mem, metric) => {
    if (!metric.name) return mem;

    mem[metric.name] = _.flatMap(metric.datapoints, d => {
      if (!_.isEmpty(d.value.histogram)) {
        return _.map(d.value.histogram.values, hist => {
          return {
            timestamp: d.timestampMs,
            value: hist.value,
            label: hist.label
          }
        });
      } else {
        return {
          timestamp: d.timestampMs,
          value: _.get(d, "value.gauge", 0)
        }
      }
    });
    return mem;
  }, {});
}
