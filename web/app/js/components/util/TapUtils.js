import _ from 'lodash';
import PropTypes from 'prop-types';

export const httpMethods = ["GET", "HEAD", "POST", "PUT", "DELETE", "CONNECT", "OPTIONS", "TRACE", "PATCH"];

export const defaultMaxRps = "1.0";

export const tapQueryPropType = PropTypes.shape({
  resource: PropTypes.string,
  namespace: PropTypes.string,
  toResource: PropTypes.string,
  toNamespace: PropTypes.string,
  method: PropTypes.string,
  path: PropTypes.string,
  scheme: PropTypes.string,
  authority: PropTypes.string,
  maxRps: PropTypes.string
});

export const processTapEvent = jsonString => {
  let d = JSON.parse(jsonString);
  d.source.str = publicAddressToString(_.get(d, "source.ip.ipv4"), d.source.port);
  d.destination.str = publicAddressToString(_.get(d, "destination.ip.ipv4"), d.destination.port);

  switch (d.proxyDirection) {
    case "INBOUND":
      d.tls = _.get(d, "sourceMeta.labels.tls", "");
      break;
    case "OUTBOUND":
      d.tls = _.get(d, "destinationMeta.labels.tls", "");
      break;
    default:
      // too old for TLS
  }

  if (_.isNil(d.http)) {
    this.setState({ error: "Undefined request type"});
  } else {
    if (!_.isNil(d.http.requestInit)) {
      d.eventType = "req";
      d.id = `${_.get(d, "http.requestInit.id.base")}:${_.get(d, "http.requestInit.id.stream")} `;
    } else if (!_.isNil(d.http.responseInit)) {
      d.eventType = "rsp";
      d.id = `${_.get(d, "http.responseInit.id.base")}:${_.get(d, "http.responseInit.id.stream")} `;
    } else if (!_.isNil(d.http.responseEnd)) {
      d.eventType = "end";
      d.id = `${_.get(d, "http.responseEnd.id.base")}:${_.get(d, "http.responseEnd.id.stream")} `;
    }
  }

  return d;
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
const publicAddressToString = (ipv4, port) => {
  let octets = decodeIPToOctets(ipv4);
  return octets.join(".") + ":" + port;
};
