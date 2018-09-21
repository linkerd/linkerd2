import _ from 'lodash';
import AddResources from './AddResources.jsx';
import ErrorBanner from './ErrorBanner.jsx';
import MetricsTable from './MetricsTable.jsx';
import Octopus from './Octopus.jsx';
import { processNeighborData } from './util/TapUtils.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import { Spin } from 'antd';
import TopModule from './TopModule.jsx';
import { withContext } from './util/AppContext.jsx';
import { emptyMetric, processSingleResourceRollup } from './util/MetricUtils.jsx';
import { resourceTypeToCamelCase, singularResource } from './util/Utils.js';
import 'whatwg-fetch';

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
    match: PropTypes.shape({}).isRequired,
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
        upstream: {},
        downstream: {}
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

  componentWillReceiveProps(newProps) {
    // React won't unmount this component when switching resource pages so we need to clear state
    this.api.cancelCurrentRequests();
    this.unmeshedSources = {};
    this.setState(this.getInitialState(newProps.match, newProps.pathPrefix));
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
        let podBelongsToResource = _.reduce(podListRsp.pods, (mem, pod) => {
          if (_.get(pod, resourceTypeToCamelCase(resource.type)) === resource.namespace + "/" + resource.name) {
            mem[pod.name] = true;
          }

          return mem;
        }, {});

        let podMetricsForResource = _.filter(podMetrics, pod => podBelongsToResource[pod.namespace + "/" + pod.name]);
        let resourceIsMeshed = true;
        if (!_.isEmpty(this.state.resourceMetrics)) {
          resourceIsMeshed = _.get(this.state.resourceMetrics, '[0].pods.meshedPods') > 0;
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
      return <Spin size="large" />;
    }

    let topQuery = {
      resource: this.state.resourceType + "/" + this.state.resourceName,
      namespace: this.state.namespace
    };

    let unmeshed = _.map(this.state.unmeshedSources, d => {
      return _.merge(emptyMetric, d, { unmeshed: true });
    });

    let upstreams = _.concat(this.state.neighborMetrics.upstream, unmeshed);

    return (
      <div>
        {
          this.state.resourceIsMeshed ? null :
          <div className="page-section">
            <AddResources
              resourceName={this.state.resourceName}
              resourceType={this.state.resourceType} />
          </div>
        }

        <div className="page-section">
          <Octopus
            resource={this.state.resourceMetrics[0]}
            neighbors={this.state.neighborMetrics}
            unmeshedSources={_.values(this.state.unmeshedSources)}
            api={this.api} />
        </div>

        {
          !this.state.resourceIsMeshed ? null :
          <div className="page-section">
            <TopModule
              pathPrefix={this.props.pathPrefix}
              query={topQuery}
              startTap={true}
              updateNeighbors={this.updateNeighborsFromTapData}
              maxRowsToDisplay={10} />
          </div>
        }

        { _.isEmpty(upstreams) ? null : (
          <div className="page-section">
            <h2 className="subsection-header">Inbound</h2>
            <MetricsTable
              resource={this.state.resource.type}
              metrics={upstreams} />
          </div>
          )
        }

        { _.isEmpty(this.state.neighborMetrics.downstream) ? null : (
          <div className="page-section">
            <h2 className="subsection-header">Outbound</h2>
            <MetricsTable
              resource={this.state.resource.type}
              metrics={this.state.neighborMetrics.downstream} />
          </div>
          )
        }

        {
          this.state.resource.type === "pod" ? null : (
            <div className="page-section">
              <h2 className="subsection-header">Pods</h2>
              <MetricsTable
                resource="pod"
                metrics={this.state.podMetrics} />
            </div>
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
