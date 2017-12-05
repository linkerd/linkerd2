import * as d3 from 'd3';
import _ from 'lodash';

// Grid constants
export const baseWidth = 8; // design base width of 8px
export const rowGutter = 3 * baseWidth;

// Formatters
const requestsFormatter = d3.format(",.3");
const successRateFormatter = d3.format(".2%");
const latencyFormatter = d3.format(",");

export const metricToFormatter = {
  "REQUEST_RATE": m => `${_.isNil(m) ? "---" : requestsFormatter(m)} RPS`,
  "SUCCESS_RATE": m => _.isNil(m) ? "---" : successRateFormatter(m),
  "LATENCY": m => `${_.isNil(m) ? "---" : latencyFormatter(m)} ms`
}

// Classname utilities
export const toClassName = name => {
  if (!name) return "";
  return _.lowerCase(name).replace(/[^a-zA-Z0-9]/g, "_");
}
