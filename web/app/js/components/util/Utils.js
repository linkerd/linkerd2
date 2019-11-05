import _has from 'lodash/has';
import _isNil from 'lodash/isNil';
import _lowerCase from 'lodash/lowerCase';
import _startCase from 'lodash/startCase';
import { format as d3Format } from 'd3-format';

/*
* Display grid constants
*/
export const baseWidth = 8; // design base width of 8px
export const rowGutter = 3 * baseWidth;

/*
* Number formatters
*/
const successRateFormatter = d3Format(".2%");
const commaFormatter = d3Format(",");
const secondsFormatter = d3Format(",.3s");

export const formatWithComma = m => {
  if (_isNil(m)) {
    return "---";
  } else {
    return commaFormatter(m);
  }
};

export const formatLatencyMs = m => {
  if (_isNil(m)) {
    return "---";
  } else {
    return `${formatLatencySec(m / 1000)}`;
  }
};

const niceLatency = l => commaFormatter(Math.round(l));

export const formatLatencySec = latency => {
  let s = parseFloat(latency);
  if (_isNil(s)) {
    return "---";
  } else if (s === parseFloat(0.0)) {
    return "0 s";
  } else if (s < 0.001) {
    return `${niceLatency(s * 1000 * 1000)} µs`;
  } else if (s < 1.0) {
    return `${niceLatency(s * 1000)} ms`;
  } else {
    return `${secondsFormatter(s)} s`;
  }
};

export const metricToFormatter = {
  "REQUEST_RATE": m => _isNil(m) ? "---" : styleNum(m, " RPS", true),
  "SUCCESS_RATE": m => _isNil(m) ? "---" : successRateFormatter(m),
  "LATENCY": formatLatencyMs,
  "UNTRUNCATED": m => styleNum(m, "", false),
  "BYTES": m => _isNil(m) ? "---" : styleNum(m, "B/s", true),
  "NO_UNIT": m => _isNil(m) ? "---" : styleNum(m, "", true)
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
  return Math.round(num * Math.pow(10, dec)) / Math.pow(10, dec);
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
  if (!name) { return ""; }
  return _lowerCase(name).replace(/[^a-zA-Z0-9]/g, "_");
};

/*
 Create regex string from user input for a filter
*/
export const regexFilterString = input => {
  // make input lower case and strip out unwanted characters
  input = input.replace(/[^A-Z0-9/.\-_*]/gi, "").toLowerCase();
  // replace "*" in input with wildcard
  return new RegExp(input.replace(/[*]/g, ".+"));
};

/*
  Nicely readable names for the stat resources
*/
export const friendlyTitle = singularOrPluralResource => {
  let resource = singularResource(singularOrPluralResource);
  let titleCase = _startCase(resource);
  if (resource === "replicationcontroller") {
    titleCase = _startCase("replication controller");
  } else if (resource === "daemonset") {
    titleCase = _startCase("daemon set");
  } else if (resource === "statefulset") {
    titleCase = _startCase("stateful set");
  } else if (resource === "trafficsplit") {
    titleCase = _startCase("traffic split");
  } else if (resource === "cronjob") {
    titleCase = _startCase("cron job");
  } else if (resource === "replicaset") {
    titleCase = _startCase("replica set");
  }

  let titles = { singular: titleCase };
  if (resource === "authority") {
    titles.plural = "Authorities";
  } else {
    titles.plural = titles.singular + "s";
  }
  return titles;
};

/*
  Get a singular resource name from a plural resource
*/
export const singularResource = resource => {
  if (resource === "authorities") {
    return "authority";
  } else { return resource.replace(/s$/, ""); }
};

/*
  Get the resource type from the /pods response, whose json
  is camelCased.
*/
const camelCaseLookUp = {
  "replicaset": "replicaSet",
  "replicationcontroller": "replicationController",
  "statefulset": "statefulSet",
  "trafficsplit": "trafficSplit",
  "daemonset": "daemonSet",
  "cronjob": "cronJob",
};

export const resourceTypeToCamelCase = resource => camelCaseLookUp[resource] || resource;

/*
  A simplified version of ShortNameFromCanonicalResourceName
*/
export const shortNameLookup = {
  "deployment": "deploy",
  "daemonset": "ds",
  "namespace": "ns",
  "pod": "po",
  "replicationcontroller": "rc",
  "replicaset": "rs",
  "service": "svc",
  "statefulset": "sts",
  "trafficsplit": "ts",
  "job": "job",
  "authority": "au",
  "cronjob": "cj",
};

export const podOwnerLookup = {
  "deployment": "deploy",
  "daemonset": "ds",
  "replicationcontroller": "rc",
  "replicaset": "rs",
  "statefulset": "sts",
  "cronjob": "cj",
};

export const toShortResourceName = name => shortNameLookup[name] || name;

export const displayName = resource => `${toShortResourceName(resource.type)}/${resource.name}`;

export const isResource = name => {
  let singularResourceName = singularResource(name);
  return _has(shortNameLookup, singularResourceName);
};

/*
  produce octets given an ip address
*/
const decodeIPToOctets = ip => {
  ip = parseInt(ip, 10);
  return [
    ip >> 24 & 255,
    ip >> 16 & 255,
    ip >> 8 & 255,
    ip & 255
  ];
};

/*
  converts an address to an ipv4 formatted host:port pair
*/
export const publicAddressToString = (ipv4, port) => {
  let octets = decodeIPToOctets(ipv4);
  return octets.join(".") + ":" + port;
};

export const getSrClassification = sr => {
  if (sr < 0.9) {
    return "status-poor";
  } else if (sr < 0.95) {
    return "status-ok";
  } else { return "status-good"; }
};
