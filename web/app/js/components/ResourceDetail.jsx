import 'whatwg-fetch';
import { emptyMetric, processMultiResourceRollup, processSingleResourceRollup } from './util/MetricUtils.jsx';
import { resourceTypeToCamelCase, singularResource } from './util/Utils.js';
import AddResources from './AddResources.jsx';
import ErrorBanner from './ErrorBanner.jsx';
import Grid from '@material-ui/core/Grid';
import MetricsTable from './MetricsTable.jsx';
import Octopus from './Octopus.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import SimpleChip from './util/Chip.jsx';
import Spinner from './util/Spinner.jsx';
import TopRoutesTabs from './TopRoutesTabs.jsx';
import Typography from '@material-ui/core/Typography';
import _filter from 'lodash/filter';
import _get from 'lodash/get';
import _isEmpty from 'lodash/isEmpty';
import _isEqual from 'lodash/isEqual';
import _isNil from 'lodash/isNil';
import _merge from 'lodash/merge';
import _reduce from 'lodash/reduce';
import { withContext } from './util/AppContext.jsx';

// if there has been no traffic for some time, show a warning
const showNoTrafficMsgDelayMs = 6000;

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
      lastMetricReceivedTime: Date.now(),
      isTcpOnly: false, // whether this resource only has TCP traffic
      pollingInterval: 2000,
      resourceMetrics: [],
      podMetrics: [], // metrics for all pods whose owner is this resource
      upstreamMetrics: {}, // metrics for resources who send traffic to this resource
      downstreamMetrics: {}, // metrics for resources who this resouce sends traffic to
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

  getDisplayMetrics(metricsByResource) {
    // if we're displaying a pod detail page, only display pod metrics
    // if we're displaying another type of resource page, display metrics for
    // rcs, deploys, replicasets, etc but not pods or authorities
    let shouldExclude = this.state.resourceType === "pod" ?
      r => r !== "pod" :
      r => r === "pod" || r === "authority" || r === "service";
    return _reduce(metricsByResource, (mem, resourceMetrics, resource) => {
      if (shouldExclude(resource)) {
        return mem;
      }
      return mem.concat(resourceMetrics);
    }, []);
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
        `${this.api.urlsForResource(resource.type, resource.namespace, true)}&resource_name=${resource.name}`
      ),
      // list of all pods in this namespace (hack since we can't currently query for all pods in a resource)
      this.api.fetchPods(resource.namespace),
      // metrics for all pods in this namespace (hack, continued)
      this.api.fetchMetrics(
        `${this.api.urlsForResource("pod", resource.namespace, true)}`
      ),
      // upstream resources of this resource (meshed traffic only)
      this.api.fetchMetrics(
        `${this.api.urlsForResource("all")}&to_name=${resource.name}&to_type=${resource.type}&to_namespace=${resource.namespace}`
      ),
      // downstream resources of this resource (meshed traffic only)
      this.api.fetchMetrics(
        `${this.api.urlsForResource("all")}&from_name=${resource.name}&from_type=${resource.type}&from_namespace=${resource.namespace}`
      )
    ]);

    Promise.all(this.api.getCurrentPromises())
      .then(([resourceRsp, podListRsp, podMetricsRsp, upstreamRsp, downstreamRsp]) => {
        let resourceMetrics = processSingleResourceRollup(resourceRsp);
        let podMetrics = processSingleResourceRollup(podMetricsRsp);
        let upstreamMetrics = processMultiResourceRollup(upstreamRsp);
        let downstreamMetrics = processMultiResourceRollup(downstreamRsp);

        // INEFFICIENT: get metrics for all the pods belonging to this resource.
        // Do this by querying for metrics for all pods in this namespace and then filtering
        // out those pods whose owner is not this resource
        // TODO: fix (#1467)
        let resourceName = resource.namespace + "/" + resource.name;
        let podMetricsForResource;

        if (resource.type === "pod") {
          // get only info for the pod whose ResourceDetail is being shown
          // pod.name in podMetrics is of the form `pod-name`
          podMetricsForResource = _filter(podMetrics, pod => pod.name === resource.name);
        } else {
          let podBelongsToResource = _reduce(podListRsp.pods, (mem, pod) => {
            if (_get(pod, resourceTypeToCamelCase(resource.type)) === resourceName) {
              // pod.name in podListRsp is of the form `namespace/pod-name`
              mem[pod.name] = true;
            }

            return mem;
          }, {});

          // get all pods whose owner is this resource
          podMetricsForResource = _filter(podMetrics, pod => podBelongsToResource[pod.namespace + "/" + pod.name]);
        }

        let resourceIsMeshed = true;
        if (!_isEmpty(this.state.resourceMetrics)) {
          resourceIsMeshed = _get(this.state.resourceMetrics, '[0].pods.meshedPods') > 0;
        }

        let hasHttp = false;
        let hasTcp = false;
        let metric = resourceMetrics[0];
        if (!_isEmpty(metric)) {
          hasHttp = !_isNil(metric.requestRate) && !_isEmpty(metric.latency);

          if (!_isEmpty(metric.tcp)) {
            let { tcp } = metric;
            hasTcp = tcp.openConnections > 0 || tcp.readBytes > 0 || tcp.writeBytes > 0;
          }
        }

        let isTcpOnly = !hasHttp && hasTcp;

        // figure out when the last traffic this resource received was so we can show a no traffic message
        let lastMetricReceivedTime = this.state.lastMetricReceivedTime;
        if (hasHttp || hasTcp) {
          lastMetricReceivedTime = Date.now();
        }

        this.setState({
          resourceMetrics,
          resourceIsMeshed,
          podMetrics: podMetricsForResource,
          upstreamMetrics,
          downstreamMetrics,
          lastMetricReceivedTime,
          isTcpOnly,
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

  updateUnmeshedSources = obj => {
    this.unmeshedSources = obj;
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
      lastMetricReceivedTime,
      isTcpOnly,
    } = this.state;

    let query = {
      resourceName,
      resourceType,
      namespace
    };

    let unmeshed = _filter(unmeshedSources, d => d.type !== "pod")
      .map(d => _merge({}, emptyMetric, d, {
        unmeshed: true,
        pods: {
          totalPods: d.pods.length,
          meshedPods: 0
        }
      }));

    let upstreamMetrics = this.getDisplayMetrics(this.state.upstreamMetrics);
    let downstreamMetrics = this.getDisplayMetrics(this.state.downstreamMetrics);

    let upstreams = upstreamMetrics.concat(unmeshed);

    let showNoTrafficMsg = resourceIsMeshed && (Date.now() - lastMetricReceivedTime > showNoTrafficMsgDelayMs);

    return (
      <div>
        <Grid container justify="space-between" alignItems="center">
          <Grid item><Typography variant="h5">{resourceType}/{resourceName}</Typography></Grid>
          <Grid item>
            <Grid container spacing={8}>
              {showNoTrafficMsg ? <Grid item><SimpleChip label="no traffic" type="warning" /></Grid> : null}
              <Grid item>
                {resourceIsMeshed ?
                  <SimpleChip label="meshed" type="good" /> :
                  <SimpleChip label="unmeshed" type="bad" />
                }
              </Grid>
            </Grid>
          </Grid>
        </Grid>

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
          neighbors={{ upstream: upstreamMetrics, downstream: downstreamMetrics }}
          unmeshedSources={Object.values(unmeshedSources)}
          api={this.api} />

        {isTcpOnly ? null : <TopRoutesTabs
          query={query}
          pathPrefix={this.props.pathPrefix}
          updateUnmeshedSources={this.updateUnmeshedSources}
          disableTop={!resourceIsMeshed} />
        }

        {_isEmpty(upstreams) ? null :
        <MetricsTable
          resource="multi_resource"
          title="Inbound"
          metrics={upstreamMetrics} />
        }

        {_isEmpty(this.state.downstreamMetrics) ? null :
        <MetricsTable
          resource="multi_resource"
          title="Outbound"
          metrics={downstreamMetrics} />
        }

        {
          this.state.resource.type === "pod" || isTcpOnly ? null :
          <MetricsTable
            resource="pod"
            title="Pods"
            metrics={this.state.podMetrics} />
        }

        <MetricsTable
          resource="pod"
          title="TCP"
          isTcpTable={true}
          metrics={this.state.podMetrics} />
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
