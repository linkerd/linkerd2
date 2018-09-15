import _ from 'lodash';
import PropTypes from 'prop-types';
import React from 'react';
import TapLink from '../TapLink.jsx';
import { Popover, Tooltip } from 'antd';
import { shortNameLookup, toShortResourceName } from './Utils.js';

export const httpMethods = ["GET", "HEAD", "POST", "PUT", "DELETE", "CONNECT", "OPTIONS", "TRACE", "PATCH"];

export const defaultMaxRps = "100.0";
export const setMaxRps = query => {
  if (!_.isEmpty(query.maxRps)) {
    query.maxRps = parseFloat(query.maxRps);
  } else {
    query.maxRps = 0; // golang unset value for maxRps
  }
};

export const tapQueryProps = {
  resource: PropTypes.string,
  namespace: PropTypes.string,
  toResource: PropTypes.string,
  toNamespace: PropTypes.string,
  method: PropTypes.string,
  path: PropTypes.string,
  scheme: PropTypes.string,
  authority: PropTypes.string,
  maxRps: PropTypes.string
};

export const tapQueryPropType = PropTypes.shape(tapQueryProps);

// from https://developer.mozilla.org/en-US/docs/Web/API/CloseEvent
export const wsCloseCodes = {
  1000: "Normal Closure",
  1001: "Going Away",
  1002: "Protocol Error",
  1003: "Unsupported Data",
  1004: "Reserved",
  1005: "No Status Recvd",
  1006: "Abnormal Closure",
  1007: "Invalid frame payload data",
  1008: "Policy Violation",
  1009: "Message too big",
  1010: "Missing Extension",
  1011: "Internal Error",
  1012: "Service Restart",
  1013: "Try Again Later",
  1014: "Bad Gateway",
  1015: "TLS Handshake"
};

/*
  Use tap data to figure out a resource's unmeshed upstreams/downstreams
*/
export const processNeighborData = (source, labels, resourceAgg, resourceType) => {
  if (_.isEmpty(labels)) {
    return resourceAgg;
  }

  let neighb = {};
  if (_.has(labels, resourceType)) {
    neighb = {
      type: resourceType,
      name: labels[resourceType],
      namespace: labels.namespace
    };
  } else if (_.has(labels, "pod")) {
    neighb = {
      type: "pod",
      name: labels.pod,
      namespace: labels.namespace
    };
  } else {
    neighb = {
      type: "ip",
      name: source.str
    };
  }

  let key = neighb.type + "/" + neighb.name;
  if (_.has(labels, "control_plane_ns")) {
    delete resourceAgg[key];
  } else {
    resourceAgg[key] = neighb;
  }

  return resourceAgg;
};

export const processTapEvent = jsonString => {
  let d = JSON.parse(jsonString);
  d.source.str = publicAddressToString(_.get(d, "source.ip.ipv4"));
  d.destination.str = publicAddressToString(_.get(d, "destination.ip.ipv4"));

  d.source.pod = _.has(d, "sourceMeta.labels.pod") ? "po/" + d.sourceMeta.labels.pod : null;
  d.destination.pod = _.has(d, "destinationMeta.labels.pod") ? "po/" + d.destinationMeta.labels.pod : null;

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
      d.eventType = "requestInit";
    } else if (!_.isNil(d.http.responseInit)) {
      d.eventType = "responseInit";
    } else if (!_.isNil(d.http.responseEnd)) {
      d.eventType = "responseEnd";
    }
    d.id = tapEventKey(d, d.eventType);
  }

  return d;
};

/*
  Use this key to associate the corresponding response with the request
  so that we can have one single event with reqInit, rspInit and rspEnd
  current key: (src, dst, stream)
*/
const tapEventKey = (d, eventType) => {
  return `${d.source.str},${d.destination.str},${_.get(d, ["http", eventType, "id", "stream"])}`;
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
  converts an address to an ipv4 formatted host
*/
const publicAddressToString = ipv4 => {
  let octets = decodeIPToOctets(ipv4);
  return octets.join(".");
};

/*
  display more human-readable information about source/destination
*/
const resourceShortLink = (resourceType, labels, ResourceLink) => (
  <ResourceLink
    resource={{ type: resourceType, name: labels[resourceType], namespace: labels.namespace}}
    linkText={toShortResourceName(resourceType) + "/" + labels[resourceType]} />
);

const resourceSection = (ip, labels, ResourceLink) => {
  return (
    <React.Fragment>
      {
        _.map(labels, (labelVal, labelName) => {
          if (_.has(shortNameLookup, labelName) && labelName !== "namespace" && labelName !== "service") {
            return <div key={labelName + "-" + labelVal}>{ resourceShortLink(labelName, labels, ResourceLink) }</div>;
          }
        })
      }
      <div>{ip}</div>
    </React.Fragment>
  );
};

export const directionColumn = d => (
  <Tooltip
    title={d}
    overlayStyle={{ fontSize: "12px" }}>
    {d === "INBOUND" ? "FROM" : "TO"}
  </Tooltip>
);

export const srcDstColumn = (d, resourceType, ResourceLink) => {
  let display = {};
  let labels = {};

  if (d.direction === "INBOUND") {
    display = d.source;
    labels = d.sourceLabels;
  } else {
    display = d.destination;
    labels = d.destinationLabels;
  }

  let content = (
    <React.Fragment>
      <h3>Source</h3>
      {resourceSection(d.source.str, d.sourceLabels, ResourceLink)}
      <br />
      <h3>Destination</h3>
      {resourceSection(d.destination.str, d.destinationLabels, ResourceLink)}
    </React.Fragment>
  );

  return (
    <Popover
      content={content}
      trigger="hover">
      <div className="src-dst-name">
        { !_.isEmpty(labels[resourceType]) ? resourceShortLink(resourceType, labels, ResourceLink) : display.str }
      </div>
    </Popover>
  );
};

export const tapLink = (d, resourceType, PrefixedLink) => {
  let namespace = d.sourceLabels.namespace;
  let resource = "";

  if (_.has(d.sourceLabels, resourceType)) {
    resource = `${resourceType}/${d.sourceLabels[resourceType]}`;
  } else if (_.has(d.sourceLabels, "pod")) {
    resource = `pod/${d.sourceLabels.pod}`;
  } else {
    return null; // can't tap a resource by IP from the web UI
  }

  let toNamespace = "";
  let toResource = "";

  if (_.has(d.destinationLabels, resourceType)) {
    toNamespace = d.destinationLabels.namespace,
    toResource = `${resourceType}/${d.destinationLabels[resourceType]}`;
  } else if (_.has(d.destinationLabels, "pod")) {
    toNamespace = d.destinationLabels.namespace,
    toResource = `${resourceType}/${d.destinationLabels.pod}`;
  }

  return (
    <TapLink
      namespace={namespace}
      resource={resource}
      toNamespace={toNamespace}
      toResource={toResource}
      path={d.path}
      PrefixedLink={PrefixedLink} />
  );
};
