import 'whatwg-fetch';

import { Link } from 'react-router-dom';
import PropTypes from 'prop-types';
import React from 'react';
import _ from 'lodash';

const checkFetchOk = resp => {
  if (resp.ok) {
    return resp;
  }

  return resp.json().then(error => {
    throw {
      status: resp.status,
      url: resp.url,
      statusText:resp.statusText,
      error: error.error
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
      error => hasCanceled_ ? reject({ isCanceled: true }) : reject(error)
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
    }
  };
};

export const apiErrorPropType = PropTypes.shape({
  status: PropTypes.number,
  url: PropTypes.string,
  statusText: PropTypes.string,
  error: PropTypes.string
});

const ApiHelpers = (pathPrefix, defaultMetricsWindow = '1m') => {
  let metricsWindow = defaultMetricsWindow;
  const podsPath = `/api/pods`;
  const servicesPath = `/api/services`;

  const validMetricsWindows = {
    "10s": "10 minutes",
    "1m": "1 minute",
    "10m": "10 minutes",
    "1h": "1 hour"
  };

  const apiFetch = path => {
    if (!_.isEmpty(pathPrefix)) {
      path = `${pathPrefix}${path}`;
    }

    return makeCancelable(fetch(path), r => r.json());
  };

  const fetchMetrics = path => {
    if (path.indexOf("window") === -1) {
      if (path.indexOf("?") === -1) {
        path = `${path}?window=${getMetricsWindow()}`;
      } else {
        path = `${path}&window=${getMetricsWindow()}`;
      }
    }
    return apiFetch(path);
  };

  const fetchPods = namespace => {
    if (!_.isNil(namespace)) {
      return apiFetch(podsPath + "?namespace=" + namespace);
    }
    return apiFetch(podsPath);
  };

  const fetchServices = namespace => {
    if (!_.isNil(namespace)) {
      return apiFetch(servicesPath + "?namespace=" + namespace);
    }
    return apiFetch(servicesPath);
  };

  const getMetricsWindow = () => metricsWindow;
  const getMetricsWindowDisplayText = () => validMetricsWindows[metricsWindow];

  const setMetricsWindow = window => {
    if (!validMetricsWindows[window]) { return; }
    metricsWindow = window;
  };

  const urlsForResource = (type, namespace) => {
    // Traffic Performance Summary. This retrieves stats for the given resource.
    let baseUrl = '/api/tps-reports?resource_type=' + type;
    return !namespace ? baseUrl + '&all_namespaces=true' : baseUrl + '&namespace=' + namespace;
  };

  // maintain a list of a component's requests,
  // convenient for providing a cancel() functionality
  let currentRequests = [];
  const setCurrentRequests = cancelablePromises => {
    currentRequests = cancelablePromises;
  };
  const getCurrentPromises = () => {
    return _.map(currentRequests, 'promise');
  };
  const cancelCurrentRequests = () => {
    _.each(currentRequests, promise => {
      promise.cancel();
    });
  };

  // prefix all links in the app with `pathPrefix`
  class PrefixedLink extends React.Component {
    static defaultProps = {
      deployment: "",
      targetBlank: false,
    }

    static propTypes = {
      children: PropTypes.node.isRequired,
      deployment: PropTypes.string,
      targetBlank: PropTypes.bool,
      to: PropTypes.string.isRequired,
    }

    render() {
      let url = prefixLink(this.props.to, this.props.deployment);

      return (
        <Link
          to={url}
          {...(this.props.targetBlank ? {target:'_blank'} : {})}>
          {this.props.children}
        </Link>
      );
    }
  }

  const prefixLink = (to, controllerDeployment) => {
    let prefix = pathPrefix;
    if (!_.isEmpty(controllerDeployment)) { // add field for grafana deployment
      prefix = prefix.replace("/linkerd-web:", "/" + controllerDeployment + ":");
    }

    return `${prefix}${to}`;
  };

  const generateResourceURL = r => {
    if (r.type === "namespace") {
      return "/namespaces/" + (r.namespace || r.name);
    }

    return "/namespaces/" + r.namespace + "/" + r.type + "s/" + r.name;
  };

  // a prefixed link to a Resource Detail page
  const ResourceLink = ({resource, linkText}) => {
    return (
      <PrefixedLink to={generateResourceURL(resource)}>
        { linkText || resource.type + "/" + resource.name}
      </PrefixedLink>
    );
  };
  ResourceLink.propTypes = {
    linkText: PropTypes.string,
    resource: PropTypes.shape({
      name: PropTypes.string,
      namespace: PropTypes.string,
      type: PropTypes.string,
    })
  };
  ResourceLink.defaultProps = {
    resource: {},
    linkText: ""
  };

  return {
    fetch: apiFetch,
    fetchMetrics,
    fetchPods,
    fetchServices,
    getMetricsWindow,
    setMetricsWindow,
    getValidMetricsWindows: () => _.keys(validMetricsWindows),
    getMetricsWindowDisplayText,
    urlsForResource,
    PrefixedLink,
    prefixLink,
    ResourceLink,
    setCurrentRequests,
    getCurrentPromises,
    generateResourceURL,
    cancelCurrentRequests,
    // DO NOT USE makeCancelable, use fetch, this is only exposed for testing
    makeCancelable
  };
};

export default ApiHelpers;
