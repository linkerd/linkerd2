import { podOwnerLookup, toShortResourceName } from './Utils.js';
import BaseTable from '../BaseTable.jsx';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import Grid from '@material-ui/core/Grid';
import OpenInNewIcon from '@material-ui/icons/OpenInNew';
import Popover from '../Popover.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import TapLink from '../TapLink.jsx';
import Tooltip from '@material-ui/core/Tooltip';
import { Trans } from '@lingui/macro';
import _each from 'lodash/each';
import _get from 'lodash/get';
import _has from 'lodash/has';
import _isEmpty from 'lodash/isEmpty';
import _isNil from 'lodash/isNil';
import _map from 'lodash/map';
import _merge from 'lodash/merge';
import _size from 'lodash/size';
import _take from 'lodash/take';
import { faLongArrowAltRight } from '@fortawesome/free-solid-svg-icons/faLongArrowAltRight';

export const httpMethods = ['GET', 'HEAD', 'POST', 'PUT', 'DELETE', 'CONNECT', 'OPTIONS', 'TRACE', 'PATCH'];

export const defaultMaxRps = '100.0';
export const setMaxRps = query => {
  if (!_isEmpty(query.maxRps)) {
    query.maxRps = parseFloat(query.maxRps);
  } else {
    query.maxRps = 0; // golang unset value for maxRps
  }
};

// resources you can tap/top to tap all pods in the resource
export const tapResourceTypes = [
  'deployment',
  'daemonset',
  'pod',
  'replicationcontroller',
  'statefulset',
  'job',
  'replicaset',
  'cronjob',
];

// use a generator to get this object, to prevent it from being overwritten
export const emptyTapQuery = () => ({
  resource: '',
  namespace: '',
  toResource: '',
  toNamespace: '',
  method: '',
  path: '',
  scheme: '',
  authority: '',
  maxRps: '',
});

export const tapQueryProps = {
  resource: PropTypes.string,
  namespace: PropTypes.string,
  toResource: PropTypes.string,
  toNamespace: PropTypes.string,
  method: PropTypes.string,
  path: PropTypes.string,
  scheme: PropTypes.string,
  authority: PropTypes.string,
  maxRps: PropTypes.string,
  extract: PropTypes.bool,
};

export const tapQueryPropType = PropTypes.shape(tapQueryProps);

// from https://developer.mozilla.org/en-US/docs/Web/API/CloseEvent
export const wsCloseCodes = {
  1000: 'Normal Closure',
  1001: 'Going Away',
  1002: 'Protocol Error',
  1003: 'Unsupported Data',
  1004: 'Reserved',
  1005: 'No Status Recvd',
  1006: 'Abnormal Closure',
  1007: 'Invalid frame payload data',
  1008: 'Policy Violation',
  1009: 'Message too big',
  1010: 'Missing Extension',
  1011: 'Internal Error',
  1012: 'Service Restart',
  1013: 'Try Again Later',
  1014: 'Bad Gateway',
  1015: 'TLS Handshake',
};

export const WS_NORMAL_CLOSURE = 1000;
export const WS_ABNORMAL_CLOSURE = 1006;
export const WS_POLICY_VIOLATION = 1008;

/*
  Use tap data to figure out a resource's unmeshed upstreams/downstreams
*/
export const processNeighborData = (source, labels, resourceAgg, resourceType) => {
  if (_isEmpty(labels)) {
    return resourceAgg;
  }

  let neighbor = {};
  if (_has(labels, resourceType)) {
    neighbor = {
      type: resourceType,
      name: labels[resourceType],
      namespace: labels.namespace,
    };
  } else if (_has(labels, 'pod')) {
    neighbor = {
      type: 'pod',
      name: labels.pod,
      namespace: labels.namespace,
    };
  } else if (_has(labels, 'node')) {
    neighbor = {
      type: 'node',
      name: labels.node,
    };
  } else {
    neighbor = {
      type: 'ip',
      name: source.str,
    };
  }

  // keep track of pods under this resource to display the number of unmeshed source pods
  neighbor.pods = {};
  if (labels.pod) {
    neighbor.pods[labels.pod] = true;
  }

  const key = `${neighbor.type}/${neighbor.name}`;
  if (_has(labels, 'control_plane_ns')) {
    delete resourceAgg[key];
  } else {
    if (_has(resourceAgg, key)) {
      _merge(neighbor.pods, resourceAgg[key].pods);
    }
    resourceAgg[key] = neighbor;
  }

  return resourceAgg;
};

/*
  Use this key to associate the corresponding response with the request
  so that we can have one single event with reqInit, rspInit and rspEnd
  current key: (src, dst, stream)
*/
const tapEventKey = (d, eventType) => {
  return `${d.source.str},${d.destination.str},${_get(d, ['http', eventType, 'id', 'stream'])}`;
};

/*
  produce octets given an ip address
*/
const decodeIPToOctets = ip => {
  const ip_ = parseInt(ip, 10);

  return [
    (ip_ >> 24) & 255,
    (ip_ >> 16) & 255,
    (ip_ >> 8) & 255,
    ip_ & 255,
  ];
};

/*
  converts an address to an ipv4 formatted host
*/
const publicAddressToString = ipv4 => {
  const octets = decodeIPToOctets(ipv4);
  return octets.join('.');
};

/*
  display more human-readable information about source/destination
*/
const resourceShortLink = (resourceType, labels, ResourceLink) => (
  <ResourceLink
    key={`${labels[resourceType]}-${labels.namespace}`}
    resource={{ type: resourceType, name: labels[resourceType], namespace: labels.namespace }}
    linkText={`${toShortResourceName(resourceType)}/${labels[resourceType]}`} />
);

export const extractPodOwner = labels => {
  let podOwner = '';
  _each(labels, (labelVal, labelName) => {
    if (_has(podOwnerLookup, labelName)) {
      podOwner = `${labelName}/${labelVal}`;
    }
  });
  return podOwner;
};

export const processTapEvent = jsonString => {
  const d = JSON.parse(jsonString);

  d.source.str = publicAddressToString(_get(d, 'source.ip.ipv4'));
  d.source.pod = _get(d, 'sourceMeta.labels.pod', null);
  d.source.owner = extractPodOwner(d.sourceMeta.labels);
  d.source.namespace = _get(d, 'sourceMeta.labels.namespace', null);

  d.destination.str = publicAddressToString(_get(d, 'destination.ip.ipv4'));
  d.destination.pod = _get(d, 'destinationMeta.labels.pod', null);
  d.destination.owner = extractPodOwner(d.destinationMeta.labels);
  d.destination.namespace = _get(d, 'destinationMeta.labels.namespace', null);

  if (_isNil(d.http)) {
    this.setState({ error: 'Undefined request type' });
  } else {
    if (!_isNil(d.http.requestInit)) {
      d.eventType = 'requestInit';
    } else if (!_isNil(d.http.responseInit)) {
      d.eventType = 'responseInit';
    } else if (!_isNil(d.http.responseEnd)) {
      d.eventType = 'responseEnd';
    }
    d.id = tapEventKey(d, d.eventType);
  }

  return d;
};

const displayLimit = 3; // how many upstreams/downstreams to display in the popover table
const popoverSrcDstColumns = [
  { title: <Trans>columnTitleSource</Trans>, dataIndex: 'source' },
  { title: '', key: 'arrow', render: () => <FontAwesomeIcon icon={faLongArrowAltRight} /> },
  { title: <Trans>columnTitleDestination</Trans>, dataIndex: 'destination' },
];

const getPodOwner = (labels, ResourceLink) => {
  const podOwner = extractPodOwner(labels);
  if (!podOwner) {
    return null;
  } else {
    const [labelName] = podOwner.split('/');
    return (
      <div className="popover-td">
        { resourceShortLink(labelName, labels, ResourceLink) }
      </div>
    );
  }
};

const getPodList = (endpoint, display, labels, ResourceLink) => {
  let podList = '---';
  if (!display) {
    if (endpoint.pod) {
      podList = resourceShortLink('pod', { pod: endpoint.pod, namespace: labels.namespace }, ResourceLink);
    }
  } else if (!_isEmpty(display.pods)) {
    podList = (
      <React.Fragment>
        {
          _map(display.pods, (namespace, pod, i) => {
            if (i > displayLimit) {
              return null;
            } else {
              return <div key={pod}>{resourceShortLink('pod', { pod, namespace }, ResourceLink)}</div>;
            }
          })
        }
        { (_size(display.pods) > displayLimit ? '...' : '') }
      </React.Fragment>
    );
  }
  return <div className="popover-td">{podList}</div>;
};

// display consists of a list of ips and pods for aggregated displays (Top)
const getIpList = (endpoint, display) => {
  let ipList = endpoint.str;
  if (display) {
    ipList = _take(Object.keys(display.ips), displayLimit).join(', ') +
      (_size(display.ips) > displayLimit ? '...' : '');
  }
  return <div className="popover-td">{ipList}</div>;
};

const popoverResourceTable = (d, ResourceLink) => { // eslint-disable-line no-unused-vars
  const tableData = [
    {
      source: getPodOwner(d.sourceLabels, ResourceLink),
      destination: getPodOwner(d.destinationLabels, ResourceLink),
      key: 'podOwner',
    },
    {
      source: getPodList(d.source, d.sourceDisplay, d.sourceLabels, ResourceLink),
      destination: getPodList(d.destination, d.destinationDisplay, d.destinationLabels, ResourceLink),
      key: 'podList',
    },
    {
      source: getIpList(d.source, d.sourceDisplay),
      destination: getIpList(d.destination, d.destinationDisplay),
      key: 'ipList',
    },
  ];

  return (
    <BaseTable
      tableColumns={popoverSrcDstColumns}
      tableRows={tableData}
      tableClassName="metric-table" />
  );
};

export const directionColumn = d => (
  <Tooltip title={d === 'INBOUND' ? <Trans>tooltipInbound</Trans> : <Trans>tooltipOutbound</Trans>} placement="right">
    <span>{d === 'INBOUND' ? <Trans>columnTitleFrom</Trans> : <Trans>columnTitleTo</Trans>}</span>
  </Tooltip>
);

export const extractDisplayName = d => {
  let display = {};
  let labels = {};

  if (d.direction === 'INBOUND') {
    display = d.source;
    labels = d.sourceLabels;
  } else {
    display = d.destination;
    labels = d.destinationLabels;
  }
  return [labels, display];
};

export const srcDstColumn = (d, resourceType, ResourceLink) => {
  const [labels, display] = extractDisplayName(d);

  const link = (
    !_isEmpty(labels[resourceType]) ?
      resourceShortLink(resourceType, labels, ResourceLink) :
      display.str
  );

  const linkFn = e => {
    e.preventDefault();
  };

  const baseContent = (
    <OpenInNewIcon fontSize="small" style={{ color: 'var(--linkblue)' }} onClick={linkFn} />
  );

  return (
    <Grid
      container
      direction="row"
      alignItems="center"
      spacing={1}>
      <Grid item>
        {link}
      </Grid>
      <Grid item>
        <Popover
          popoverContent={(popoverResourceTable(d, ResourceLink))}
          baseContent={baseContent} />
      </Grid>
    </Grid>
  );
};

export const tapLink = (d, resourceType, PrefixedLink) => {
  let disabled = false;
  const namespace = d.sourceLabels.namespace;
  let resource = '';

  if (!d.meshed) {
    disabled = true;
  } else if (_has(d.sourceLabels, resourceType)) {
    resource = `${resourceType}/${d.sourceLabels[resourceType]}`;
  } else if (_has(d.sourceLabels, 'pod')) {
    resource = `pod/${d.sourceLabels.pod}`;
  } else {
    // can't tap a resource by IP from the web UI
    disabled = true;
  }

  let toNamespace = '';
  let toResource = '';

  if (_has(d.destinationLabels, resourceType)) {
    toNamespace = d.destinationLabels.namespace;
    toResource = `${resourceType}/${d.destinationLabels[resourceType]}`;
  } else if (_has(d.destinationLabels, 'pod')) {
    toNamespace = d.destinationLabels.namespace;
    toResource = `${resourceType}/${d.destinationLabels.pod}`;
  }

  return (
    <TapLink
      namespace={namespace}
      resource={resource}
      toNamespace={toNamespace}
      toResource={toResource}
      path={d.path}
      PrefixedLink={PrefixedLink}
      disabled={disabled} />
  );
};
