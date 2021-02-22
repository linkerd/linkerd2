import 'whatwg-fetch';

import { Link } from 'react-router-dom';
import PropTypes from 'prop-types';
import React from 'react';
import _each from 'lodash/each';
import _isEmpty from 'lodash/isEmpty';
import _isNil from 'lodash/isNil';
import _map from 'lodash/map';

const checkFetchOk = resp => {
  if (resp.ok) {
    return resp;
  }

  return resp.json().then(error => {
    throw {
      status: resp.status,
      url: resp.url,
      statusText: resp.statusText,
      error: error.error,
    };
  });
};

// makeCancelable from @istarkov
// https://reactjs.org/blog/2015/12/16/ismounted-antipattern.html
const makeCancelable = (promise, onSuccess) => {
  let hasCanceled_ = false;

  const wrappedPromise = new Promise((resolve, reject) => {
    return promise.then(
      result => hasCanceled_ ? reject({ isCanceled: true }) : resolve(result),
      error => hasCanceled_ ? reject({ isCanceled: true }) : reject(error),
    );
  })
    .then(checkFetchOk)
    .then(onSuccess);

  return {
    promise: wrappedPromise,
    cancel: () => {
      hasCanceled_ = true;
    },
    status: () => {
      return hasCanceled_;
    },
  };
};

export const apiErrorPropType = PropTypes.shape({
  status: PropTypes.number,
  url: PropTypes.string,
  statusText: PropTypes.string,
  error: PropTypes.string,
});

const ApiHelpers = (pathPrefix, defaultMetricsWindow = '1m') => {
  let metricsWindow = defaultMetricsWindow;
  const podsPath = '/api/pods';
  const servicesPath = '/api/services';
  const edgesPath = '/api/edges';
  const gatewaysPath = '/api/gateways';
  const l5dExtensionsPath = '/api/extension';

  const validMetricsWindows = {
    '10s': '10 minutes',
    '1m': '1 minute',
    '10m': '10 minutes',
    '1h': '1 hour',
  };

  // for getting json api results
  const apiFetch = path => {
    let path_ = path;
    if (!_isEmpty(pathPrefix)) {
      path_ = `${pathPrefix}${path_}`;
    }

    return makeCancelable(fetch(path_), r => r.json());
  };

  // for getting yaml api results
  const apiFetchYAML = path => {
    let path_ = path;
    if (!_isEmpty(pathPrefix)) {
      path_ = `${pathPrefix}${path_}`;
    }

    return makeCancelable(fetch(path_), r => r.text());
  };

  // for getting non-json results
  const prefixedUrl = path => {
    let path_ = path;
    if (!_isEmpty(pathPrefix)) {
      path_ = `${pathPrefix}${path_}`;
    }

    return path_;
  };

  const getMetricsWindow = () => metricsWindow;
  const getMetricsWindowDisplayText = () => validMetricsWindows[metricsWindow];

  const setMetricsWindow = window => {
    if (!validMetricsWindows[window]) { return; }
    metricsWindow = window;
  };

  const fetchMetrics = path => {
    let path_ = path;
    if (path_.indexOf('window') === -1) {
      if (path_.indexOf('?') === -1) {
        path_ = `${path_}?window=${getMetricsWindow()}`;
      } else {
        path_ = `${path_}&window=${getMetricsWindow()}`;
      }
    }
    return apiFetch(path_);
  };

  const fetchPods = namespace => {
    if (!_isNil(namespace)) {
      return apiFetch(`${podsPath}?namespace=${namespace}`);
    }
    return apiFetch(podsPath);
  };

  const fetchServices = namespace => {
    if (!_isNil(namespace)) {
      return apiFetch(`${servicesPath}?namespace=${namespace}`);
    }
    return apiFetch(servicesPath);
  };

  const fetchEdges = (namespace, resourceType) => {
    return apiFetch(`${edgesPath}?resource_type=${resourceType}&namespace=${namespace}`);
  };

  const fetchGateways = () => {
    return apiFetch(gatewaysPath);
  };

  const fetchCheck = () => {
    return apiFetch('/api/check');
  };

  const fetchExtension = name => {
    return apiFetch(`${l5dExtensionsPath}?extension_name=${name}`);
  };

  const fetchResourceDefinition = (namespace, resourceType, resourceName) => {
    return apiFetchYAML(`/api/resource-definition?namespace=${namespace}&resource_type=${resourceType}&resource_name=${resourceName}`);
  };

  const urlsForResource = (type, namespace, includeTcp) => {
    // Traffic Performance Summary. This retrieves stats for the given resource.
    let resourceUrl = `/api/tps-reports?resource_type=${type}`;

    if (_isEmpty(namespace) || namespace === '_all') {
      resourceUrl += '&all_namespaces=true';
    } else {
      resourceUrl += `&namespace=${namespace}`;
    }
    if (includeTcp) {
      resourceUrl += '&tcp_stats=true';
    }

    return resourceUrl;
  };

  const urlsForResourceNoStats = (type, namespace) => {
    // Traffic Performance Summary. This retrieves (non-Prometheus) stats for the given resource.
    let resourceUrl = `/api/tps-reports?skip_stats=true&resource_type=${type}`;

    if (_isEmpty(namespace) || namespace === '_all') {
      resourceUrl += '&all_namespaces=true';
    } else {
      resourceUrl += `&namespace=${namespace}`;
    }

    return resourceUrl;
  };

  // maintain a list of a component's requests,
  // convenient for providing a cancel() functionality
  let currentRequests = [];
  const setCurrentRequests = cancelablePromises => {
    currentRequests = cancelablePromises;
  };
  const getCurrentPromises = () => {
    return _map(currentRequests, r => r.promise);
  };
  const cancelCurrentRequests = () => {
    _each(currentRequests, promise => {
      promise.cancel();
    });
  };

  const prefixLink = to => `${pathPrefix}${to}`;

  // prefix all links in the app with `pathPrefix`
  const PrefixedLink = ({ to, targetBlank, children }) => {
    const url = prefixLink(to);

    return (
      <Link
        to={url}
        {...(targetBlank ? { target: '_blank' } : {})}>
        {children}
      </Link>
    );
  };

  PrefixedLink.propTypes = {
    children: PropTypes.node.isRequired,
    targetBlank: PropTypes.bool,
    to: PropTypes.string.isRequired,
  };

  PrefixedLink.defaultProps = {
    targetBlank: false,
  };

  const generateResourceURL = r => {
    if (r.type === 'namespace') {
      return `/namespaces/${r.namespace || r.name}`;
    }

    return `/namespaces/${r.namespace}/${r.type}s/${r.name}`;
  };

  // a prefixed link to a Resource Detail page
  const ResourceLink = ({ resource, linkText }) => {
    return (
      <PrefixedLink to={generateResourceURL(resource)}>
        {linkText || `${resource.type}/${resource.name}`}
      </PrefixedLink>
    );
  };
  ResourceLink.propTypes = {
    linkText: PropTypes.string,
    resource: PropTypes.shape({
      name: PropTypes.string,
      namespace: PropTypes.string,
      type: PropTypes.string,
    }),
  };
  ResourceLink.defaultProps = {
    resource: {},
    linkText: '',
  };

  return {
    fetch: apiFetch,
    prefixedUrl,
    fetchMetrics,
    fetchPods,
    fetchServices,
    fetchEdges,
    fetchGateways,
    fetchExtension,
    fetchCheck,
    fetchResourceDefinition,
    getMetricsWindow,
    setMetricsWindow,
    getValidMetricsWindows: () => Object.keys(validMetricsWindows),
    getMetricsWindowDisplayText,
    urlsForResource,
    urlsForResourceNoStats,
    PrefixedLink,
    prefixLink,
    ResourceLink,
    setCurrentRequests,
    getCurrentPromises,
    generateResourceURL,
    cancelCurrentRequests,
    // DO NOT USE makeCancelable, use fetch, this is only exposed for testing
    makeCancelable,
  };
};

export default ApiHelpers;
