import _ from 'lodash';
import * as d3 from 'd3';

/*
* Display grid constants
*/
export const baseWidth = 8; // design base width of 8px
export const rowGutter = 3 * baseWidth;

/*
* Number formatters
*/
const successRateFormatter = d3.format(".2%");
const latencySecFormatter = d3.format(".3f");
const latencyFormatter = d3.format(",");

const formatLatency = m => {
  if (_.isNil(m)) {
    return "---";
  } else if (m < 1000) {
    return `${latencyFormatter(m)} ms`;
  } else {
    return `${latencySecFormatter(m / 1000)} s`;
  }
};

export const metricToFormatter = {
  "REQUEST_RATE": m => _.isNil(m) ? "---" : styleNum(m, " RPS", true),
  "SUCCESS_RATE": m => _.isNil(m) ? "---" : successRateFormatter(m),
  "LATENCY": formatLatency,
  "UNTRUNCATED": m => styleNum(m, "", false)
};

/*
* Add commas to a number (converting it to a string in the process)
*/
export function addCommas(nStr) {
  nStr += '';
  let x = nStr.split('.');
  let x1 = x[0];
  let x2 = x.length > 1 ? '.' + x[1] : '';
  let rgx = /(\d+)(\d{3})/;
  while (rgx.test(x1)) {
    x1 = x1.replace(rgx, '$1' + ',' + '$2');
  }
  return x1 + x2;
}

/*
* Round a number to a given number of decimals
*/
export const roundNumber = (num, dec) => {
  return Math.round(num * Math.pow(10,dec)) / Math.pow(10,dec);
};

/*
* Shorten and style number
*/
export const styleNum = (number, unit = "", truncate = true) => {
  if (Number.isNaN(number)) {
    return "N/A";
  }

  if (truncate && number > 999999999) {
    number = roundNumber(number / 1000000000.0, 3);
    return addCommas(number) + "G" + unit;
  } else if (truncate && number > 999999) {
    number = roundNumber(number / 1000000.0, 3);
    return addCommas(number) + "M" + unit;
  } else if (truncate && number > 999) {
    number = roundNumber(number / 1000.0, 3);
    return addCommas(number) + "k" + unit;
  } else if (number > 999) {
    number = roundNumber(number, 0);
    return addCommas(number) + unit;
  } else {
    number = roundNumber(number, 2);
    return addCommas(number) + unit;
  }
};

/*
* Convert a string to a valid css class name
*/
export const toClassName = name => {
  if (!name) return "";
  return _.lowerCase(name).replace(/[^a-zA-Z0-9]/g, "_");
};

/*
  Definition of sort, for ant table sorting
*/
export const numericSort = (a, b) => (_.isNil(a) ? -1 : a) - (_.isNil(b) ? -1 : b);
