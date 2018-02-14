import _ from 'lodash';
import { Link } from 'react-router-dom';
import React from 'react';
import 'whatwg-fetch';

export const ApiHelpers = (pathPrefix, defaultMetricsWindow = '10m') => {
  let metricsWindow = defaultMetricsWindow;
  const podsPath = `/api/pods`;

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
    return fetch(path).then(handleFetchErr).then(r => r.json());
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

  const fetchPods = () => {
    return apiFetch(podsPath);
  };

  const handleFetchErr = resp => {
    if (!resp.ok) {
      throw Error(resp.statusText);
    }
    return resp;
  };

  const getMetricsWindow = () => metricsWindow;
  const getMetricsWindowDisplayText = () => validMetricsWindows[metricsWindow];

  const setMetricsWindow = window => {
    if (!validMetricsWindows[window]) return;
    metricsWindow = window;
  };

  const metricsUrl = `/api/metrics?`;
  const urlsForResource = {
    // all deploys (default), or a given deploy if specified
    "deployment": {
      groupBy: "targetDeploy",
      url: (deploy = null) => {
        let rollupUrl = !deploy ? metricsUrl : `${metricsUrl}&target_deploy=${deploy}`;
        let timeseriesUrl = !deploy ? `${metricsUrl}&timeseries=true` :
          `${metricsUrl}&timeseries=true&target_deploy=${deploy}`;
        return {
          ts: timeseriesUrl,
          rollup: rollupUrl
        };
      }
    },
    "upstream_deployment": {
      // all upstreams of a given deploy
      groupBy: "sourceDeploy",
      url: deploy => {
        let upstreamRollupUrl = `${metricsUrl}&aggregation=source_deploy&target_deploy=${deploy}`;
        let upstreamTimeseriesUrl = `${upstreamRollupUrl}&timeseries=true`;
        return {
          ts: upstreamTimeseriesUrl,
          rollup: upstreamRollupUrl
        };
      }
    },
    "downstream_deployment": {
      // all downstreams of a given deploy
      groupBy: "targetDeploy",
      url: deploy => {
        let downstreamRollupUrl = `${metricsUrl}&aggregation=target_deploy&source_deploy=${deploy}`;
        let downstreamTimeseriesUrl = `${downstreamRollupUrl}&timeseries=true`;
        return {
          ts: downstreamTimeseriesUrl,
          rollup: downstreamRollupUrl
        };
      }
    }
  };


  // prefix all links in the app with `pathPrefix`
  const ConduitLink = props => {
    let {to, absolute} = props;

    if (absolute) {
      return <Link to={to} target="_blank">{props.children}</Link>;
    } else {
      return <Link to={`${pathPrefix}${to}`}>{props.children}</Link>;
    }
  };

  return {
    fetch: apiFetch,
    fetchMetrics,
    fetchPods,
    getMetricsWindow,
    setMetricsWindow,
    getValidMetricsWindows: () => _.keys(validMetricsWindows),
    getMetricsWindowDisplayText,
    urlsForResource: urlsForResource,
    ConduitLink
  };
};
