import 'whatwg-fetch';

import { emptyMetric, processSingleResourceRollup } from './util/MetricUtils.jsx';
import { resourceTypeToCamelCase, singularResource } from './util/Utils.js';

import AddResources from './AddResources.jsx';
import ErrorBanner from './ErrorBanner.jsx';
import MetricsTable from './MetricsTable.jsx';
import Octopus from './Octopus.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import Spinner from './util/Spinner.jsx';
import TopRoutesTabs from './TopRoutesTabs.jsx';
import Typography from '@material-ui/core/Typography';
import _filter from 'lodash/filter';
import _get from 'lodash/get';
import _isEmpty from 'lodash/isEmpty';
import _isEqual from 'lodash/isEqual';
import _merge from 'lodash/merge';
import _reduce from 'lodash/reduce';
import { processNeighborData } from './util/TapUtils.jsx';
import { withContext } from './util/AppContext.jsx';

const getResourceFromUrl = (match, pathPrefix) => {
  let resource = {
    namespace: match.params.namespace
  };
  let regExp = RegExp(`${pathPrefix || ""}/namespaces/${match.params.namespace}/([^/]+)/([^/]+)`);
  let urlParts = match.url.match(regExp);

  resource.type = singularResource(urlParts[1]);
  resource.name = urlParts[2];

  if (match.params[resource.type] !== resource.name) {
    console.error("Failed to extract resource from URL");
  }
  return resource;
};

export class ResourceDetailBase extends React.Component {
  static propTypes = {
    api: PropTypes.shape({
      PrefixedLink: PropTypes.func.isRequired,
    }).isRequired,
    match: PropTypes.shape({
      url: PropTypes.string.isRequired
    }).isRequired,
    pathPrefix: PropTypes.string.isRequired
  }

  constructor(props) {
    super(props);
    this.api = this.props.api;
    this.unmeshedSources = {};
    this.handleApiError = this.handleApiError.bind(this);
    this.loadFromServer = this.loadFromServer.bind(this);
    this.state = this.getInitialState(props.match, props.pathPrefix);
  }

  getInitialState(match, pathPrefix) {
    let resource = getResourceFromUrl(match, pathPrefix);
    return {
      namespace: resource.namespace,
      resourceName: resource.name,
      resourceType: resource.type,
      resource,
      pollingInterval: 2000,
      resourceMetrics: [],
      podMetrics: [], // metrics for all pods whose owner is this resource
      neighborMetrics: {
        upstream: [],
        downstream: []
      },
      unmeshedSources: {},
      resourceIsMeshed: true,
      pendingRequests: false,
      loaded: false,
      error: null
    };
  }

  componentDidMount() {
    this.loadFromServer();
    this.timerId = window.setInterval(this.loadFromServer, this.state.pollingInterval);
  }

  componentDidUpdate(prevProps) {
    if (!_isEqual(prevProps.match.url, this.props.match.url)) {
      // React won't unmount this component when switching resource pages so we need to clear state
      this.api.cancelCurrentRequests();
      this.unmeshedSources = {};
      this.setState(this.getInitialState(this.props.match, this.props.pathPrefix));
    }
  }

  componentWillUnmount() {
    window.clearInterval(this.timerId);
    this.api.cancelCurrentRequests();
  }

  loadFromServer() {
    if (this.state.pendingRequests) {
      return; // don't make more requests if the ones we sent haven't completed
    }
    this.setState({ pendingRequests: true });

    let { resource } = this.state;

    this.api.setCurrentRequests([
      // inbound stats for this resource
      this.api.fetchMetrics(
        `${this.api.urlsForResource(resource.type, resource.namespace)}&resource_name=${resource.name}`
      ),
      // list of all pods in this namespace (hack since we can't currently query for all pods in a resource)
      this.api.fetchPods(resource.namespace),
      // metrics for all pods in this namespace (hack, continued)
      this.api.fetchMetrics(
        `${this.api.urlsForResource("pod", resource.namespace)}`
      ),
      // upstream resources of this resource (meshed traffic only)
      this.api.fetchMetrics(
        `${this.api.urlsForResource(resource.type)}&to_name=${resource.name}&to_type=${resource.type}&to_namespace=${resource.namespace}`
      ),
      // downstream resources of this resource (meshed traffic only)
      this.api.fetchMetrics(
        `${this.api.urlsForResource(resource.type)}&from_name=${resource.name}&from_type=${resource.type}&from_namespace=${resource.namespace}`
      )
    ]);

    Promise.all(this.api.getCurrentPromises())
      .then(([resourceRsp, podListRsp, podMetricsRsp, upstreamRsp, downstreamRsp]) => {
        let resourceMetrics = processSingleResourceRollup(resourceRsp);
        let podMetrics = processSingleResourceRollup(podMetricsRsp);
        let upstreamMetrics = processSingleResourceRollup(upstreamRsp);
        let downstreamMetrics = processSingleResourceRollup(downstreamRsp);

        // INEFFICIENT: get metrics for all the pods belonging to this resource.
        // Do this by querying for metrics for all pods in this namespace and then filtering
        // out those pods whose owner is not this resource
        // TODO: fix (#1467)
        let podBelongsToResource = _reduce(podListRsp.pods, (mem, pod) => {
          if (_get(pod, resourceTypeToCamelCase(resource.type)) === resource.namespace + "/" + resource.name) {
            mem[pod.name] = true;
          }

          return mem;
        }, {});

        let podMetricsForResource = _filter(podMetrics, pod => podBelongsToResource[pod.namespace + "/" + pod.name]);
        let resourceIsMeshed = true;
        if (!_isEmpty(this.state.resourceMetrics)) {
          resourceIsMeshed = _get(this.state.resourceMetrics, '[0].pods.meshedPods') > 0;
        }

        this.setState({
          resourceMetrics,
          resourceIsMeshed,
          podMetrics: podMetricsForResource,
          neighborMetrics: {
            upstream: upstreamMetrics,
            downstream: downstreamMetrics
          },
          loaded: true,
          pendingRequests: false,
          error: null,
          unmeshedSources: this.unmeshedSources // in place of debouncing, just update this when we update the rest of the state
        });
      })
      .catch(this.handleApiError);
  }

  handleApiError = e => {
    if (e.isCanceled) {
      return;
    }

    this.setState({
      loaded: true,
      pendingRequests: false,
      error: e
    });
  }

  updateNeighborsFromTapData = (source, sourceLabels) => {
    // store this outside of state, as updating the state upon every websocket event received
    // is very costly and causes the page to freeze up
    this.unmeshedSources = processNeighborData(source, sourceLabels, this.unmeshedSources, this.state.resource.type);
  }

  banner = () => {
    if (!this.state.error) {
      return;
    }

    return <ErrorBanner message={this.state.error} />;
  }

  content = () => {
    if (!this.state.loaded && !this.state.error) {
      return <Spinner />;
    }

    let {
      resourceName,
      resourceType,
      namespace,
      resourceMetrics,
      unmeshedSources,
      resourceIsMeshed,
      neighborMetrics
    } = this.state;

    let query = {
      resourceName,
      resourceType,
      namespace
    };

    let unmeshed = _filter(unmeshedSources, d => d.type === resourceType)
      .map(d => _merge({}, emptyMetric, d, {
        unmeshed: true,
        pods: {
          totalPods: d.pods.length,
          meshedPods: 0
        }
      }));

    let upstreams = neighborMetrics.upstream.concat(unmeshed);

    console.log(resourceMetrics[0]);
    return (
      <div>
        {
          resourceIsMeshed ? null :
          <React.Fragment>
            <AddResources
              resourceName={resourceName}
              resourceType={resourceType} />
          </React.Fragment>
        }

        <Octopus
          resource={resourceMetrics[0]}
          neighbors={neighborMetrics}
          unmeshedSources={Object.values(unmeshedSources)}
          api={this.api} />

        <TopRoutesTabs
          query={query}
          pathPrefix={this.props.pathPrefix}
          updateNeighborsFromTapData={this.updateNeighborsFromTapData}
          disableTop={!resourceIsMeshed} />


        { _isEmpty(upstreams) ? null : (
          <React.Fragment>
            <Typography variant="h5">Inbound</Typography>
            <MetricsTable
              resource={this.state.resource.type}
              metrics={upstreams} />
          </React.Fragment>
          )
        }

        { _isEmpty(this.state.neighborMetrics.downstream) ? null : (
          <React.Fragment>
            <Typography variant="h5">Outbound</Typography>
            <MetricsTable
              resource={this.state.resource.type}
              metrics={this.state.neighborMetrics.downstream} />
          </React.Fragment>
          )
        }

        {
          this.state.resource.type === "pod" ? null : (
            <React.Fragment>
              <Typography variant="h5">Pods</Typography>
              <MetricsTable
                resource="pod"
                metrics={this.state.podMetrics} />
            </React.Fragment>
          )
        }
      </div>
    );
  }

  render() {
    return (
      <div className="page-content">
        <div>
          {this.banner()}
          {this.content()}
        </div>
      </div>
    );
  }
}

export default withContext(ResourceDetailBase);
