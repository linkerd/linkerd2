import 'whatwg-fetch';
import { emptyMetric, processMultiResourceRollup, processSingleResourceRollup } from './util/MetricUtils.jsx';
import { handlePageVisibility, withPageVisibility } from './util/PageVisibility.jsx';
import { resourceTypeToCamelCase, singularResource } from './util/Utils.js';
import AddResources from './AddResources.jsx';
import EdgesTable from './EdgesTable.jsx';
import ErrorBanner from './ErrorBanner.jsx';
import Grid from '@material-ui/core/Grid';
import MetricsTable from './MetricsTable.jsx';
import Octopus from './Octopus.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import SimpleChip from './util/Chip.jsx';
import Spinner from './util/Spinner.jsx';
import TopRoutesTabs from './TopRoutesTabs.jsx';
import TrafficSplitDetail from './TrafficSplitDetail.jsx';
import { Trans } from '@lingui/macro';
import Typography from '@material-ui/core/Typography';
import _filter from 'lodash/filter';
import _get from 'lodash/get';
import _indexOf from 'lodash/indexOf';
import _isEmpty from 'lodash/isEmpty';
import _isEqual from 'lodash/isEqual';
import _isNil from 'lodash/isNil';
import _merge from 'lodash/merge';
import _reduce from 'lodash/reduce';
import { processEdges } from './util/EdgesUtils.jsx';
import { withContext } from './util/AppContext.jsx';

// if there has been no traffic for some time, show a warning
const showNoTrafficMsgDelayMs = 6000;
// resource types supported when querying API for edge data
const edgeDataAvailable = ['cronjob', 'daemonset', 'deployment', 'job', 'pod', 'replicaset', 'replicationcontroller', 'statefulset'];

const getResourceFromUrl = (match, pathPrefix) => {
  const resource = {
    namespace: match.params.namespace,
  };
  const regExp = RegExp(`${pathPrefix || ''}/namespaces/${match.params.namespace}/([^/]+)/([^/]+)`);
  const urlParts = match.url.match(regExp);

  resource.type = singularResource(urlParts[1]);
  resource.name = urlParts[2];

  if (match.params[resource.type] !== resource.name) {
    console.error('Failed to extract resource from URL');
  }
  return resource;
};

export class ResourceDetailBase extends React.Component {
  constructor(props) {
    super(props);
    this.api = props.api;
    this.unmeshedSources = {};
    this.handleApiError = this.handleApiError.bind(this);
    this.loadFromServer = this.loadFromServer.bind(this);
    this.state = this.getInitialState(props.match, props.pathPrefix);
  }

  getInitialState(match, pathPrefix) {
    const resource = getResourceFromUrl(match, pathPrefix);
    return {
      namespace: resource.namespace,
      resourceName: resource.name,
      resourceType: resource.type,
      lastMetricReceivedTime: Date.now(),
      isTcpOnly: false, // whether this resource only has TCP traffic
      pollingInterval: 2000,
      resourceMetrics: [],
      podMetrics: [], // metrics for all pods whose owner is this resource
      upstreamMetrics: {}, // metrics for resources who send traffic to this resource
      downstreamMetrics: {}, // metrics for resources who this resource sends traffic to
      unmeshedSources: {},
      resourceIsMeshed: true,
      pendingRequests: false,
      loaded: false,
      error: null,
      resourceDefinition: null,
      // queryForDefinition is set to false now due to we are not currently using
      // resource definition. This can change in the future
      queryForDefinition: false,
    };
  }

  componentDidMount() {
    this.startServerPolling();
  }

  componentDidUpdate(prevProps) {
    const { match, pathPrefix, isPageVisible } = this.props;

    if (!_isEqual(prevProps.match.url, match.url)) {
      // React won't unmount this component when switching resource pages so we need to clear state
      this.api.cancelCurrentRequests();
      this.unmeshedSources = {};
      this.resetState(match, pathPrefix);
    }

    handlePageVisibility({
      prevVisibilityState: prevProps.isPageVisible,
      currentVisibilityState: isPageVisible,
      onVisible: () => this.startServerPolling(),
      onHidden: () => this.stopServerPolling(),
    });
  }

  resetState(match, pathPrefix) {
    this.setState(this.getInitialState(match, pathPrefix));
  }

  componentWillUnmount() {
    this.stopServerPolling();
  }

  // if we're displaying a pod detail page, only display pod metrics
  // if we're displaying another type of resource page, display metrics for
  // rcs, deploys, replicasets, etc but not pods or authorities
  getDisplayMetrics(metricsByResource) {
    const { resourceType } = this.state;
    const shouldExclude = resourceType === 'pod' ?
      r => r !== 'pod' :
      r => r === 'pod' || r === 'authority' || r === 'service';
    return _reduce(metricsByResource, (mem, resourceMetrics, resource) => {
      if (shouldExclude(resource)) {
        return mem;
      }
      return mem.concat(resourceMetrics);
    }, []);
  }

  startServerPolling() {
    const { pollingInterval } = this.state;
    this.loadFromServer();
    this.timerId = window.setInterval(this.loadFromServer, pollingInterval);
  }

  stopServerPolling() {
    window.clearInterval(this.timerId);
    this.api.cancelCurrentRequests();
    this.setState({ pendingRequests: false });
  }

  loadFromServer() {
    const { pendingRequests, queryForDefinition, resourceType, namespace, resourceName, resourceDefinition, lastMetricReceivedTime } = this.state;

    if (pendingRequests) {
      return; // don't make more requests if the ones we sent haven't completed
    }
    this.setState({ pendingRequests: true });

    let apiRequests =
      [
      // inbound stats for this resource
        this.api.fetchMetrics(
          `${this.api.urlsForResource(resourceType, namespace, true)}&resource_name=${resourceName}`,
        ),
        // upstream resources of this resource (meshed traffic only)
        this.api.fetchMetrics(
          `${this.api.urlsForResource('all')}&to_name=${resourceName}&to_type=${resourceType}&to_namespace=${namespace}`,
        ),
        // downstream resources of this resource (meshed traffic only)
        this.api.fetchMetrics(
          `${this.api.urlsForResource('all')}&from_name=${resourceName}&from_type=${resourceType}&from_namespace=${namespace}`,
        ),
      ];

    // Fetch pods in a resource and their metrics (except when resource type is pod)
    if (resourceType !== 'pod') {
      // list of all pods in this namespace (hack since we can't currently query for all pods in a resource)
      apiRequests.push(this.api.fetchPods(namespace));
      // metrics for all pods in this namespace (hack, continued)
      apiRequests.push(this.api.fetchMetrics(`${this.api.urlsForResource('pod', namespace, true)}`));
    }

    if (queryForDefinition) {
      // definition for this resource
      apiRequests.push(this.api.fetchResourceDefinition(namespace, resourceType, resourceName));
    }

    if (_indexOf(edgeDataAvailable, resourceType) > 0) {
      apiRequests = apiRequests.concat([
        this.api.fetchEdges(namespace, resourceType),
      ]);
    }

    this.api.setCurrentRequests(apiRequests);

    Promise.all(this.api.getCurrentPromises())
      .then(apiResponses => {
        let podMetrics;
        let resourceRsp;
        let upstreamRsp;
        let downstreamRsp;
        let podListRsp;
        let podMetricsRsp;
        let rsp;

        if (resourceType === 'pod') {
          [resourceRsp, upstreamRsp, downstreamRsp, ...rsp] = [...apiResponses];
        } else {
          [resourceRsp, upstreamRsp, downstreamRsp, podListRsp, podMetricsRsp, ...rsp] = [...apiResponses];
          podMetrics = processSingleResourceRollup(podMetricsRsp, resourceType);
        }

        const resourceMetrics = processSingleResourceRollup(resourceRsp, resourceType);
        const upstreamMetrics = processMultiResourceRollup(upstreamRsp, resourceType);
        const downstreamMetrics = processMultiResourceRollup(downstreamRsp, resourceType);
        const newResourceDefinition = queryForDefinition ? rsp[0] : resourceDefinition;

        let edges = [];
        if (_indexOf(edgeDataAvailable, resourceType) > 0) {
          const edgesRsp = rsp[rsp.length - 1];
          edges = processEdges(edgesRsp, resourceName);
        }

        // INEFFICIENT: get metrics for all the pods belonging to this resource.
        // Do this by querying for metrics for all pods in this namespace and then filtering
        // out those pods whose owner is not this resource
        // TODO: fix (#1467)
        const resourceKey = `${namespace}/${resourceName}`;
        let podMetricsForResource;

        if (resourceType === 'pod') {
          podMetricsForResource = resourceMetrics;
        } else {
          const podBelongsToResource = _reduce(podListRsp.pods, (mem, pod) => {
            if (_get(pod, resourceTypeToCamelCase(resourceType)) === resourceKey) {
              // pod.name in podListRsp is of the form `namespace/pod-name`
              mem[pod.name] = true;
            }

            return mem;
          }, {});

          // get all pods whose owner is this resource
          podMetricsForResource = _filter(podMetrics, pod => podBelongsToResource[`${pod.namespace}/${pod.name}`]);
        }

        let resourceIsMeshed = true;
        if (!_isEmpty(resourceMetrics)) {
          resourceIsMeshed = _get(resourceMetrics, '[0].pods.meshedPods') > 0;
        }

        let hasHttp = false;
        let hasTcp = false;
        const metric = resourceMetrics[0];
        if (!_isEmpty(metric)) {
          hasHttp = !_isNil(metric.requestRate) && !_isEmpty(metric.latency);

          if (!_isEmpty(metric.tcp)) {
            const { tcp } = metric;
            hasTcp = tcp.openConnections > 0 || tcp.readBytes > 0 || tcp.writeBytes > 0;
          }
        }

        const isTcpOnly = !hasHttp && hasTcp;
        const isTrafficSplit = resourceType === 'trafficsplit';

        // figure out when the last traffic this resource received was so we can show a no traffic message
        let newLastMetricReceivedTime = lastMetricReceivedTime;
        if (hasHttp || hasTcp) {
          newLastMetricReceivedTime = Date.now();
        }

        this.setState({
          resourceMetrics,
          resourceIsMeshed,
          resourceRsp,
          podMetrics: podMetricsForResource,
          upstreamMetrics,
          downstreamMetrics,
          edges,
          lastMetricReceivedTime: newLastMetricReceivedTime,
          isTcpOnly,
          isTrafficSplit,
          loaded: true,
          pendingRequests: false,
          error: null,
          unmeshedSources: this.unmeshedSources, // in place of debouncing, just update this when we update the rest of the state
          resourceDefinition: newResourceDefinition,
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
      error: e,
    });
  }

  updateUnmeshedSources = obj => {
    this.unmeshedSources = obj;
  }

  banner = () => {
    const { error } = this.state;
    return error ? <ErrorBanner message={error} /> : null;
  }

  content = () => {
    const {
      resourceName,
      resourceType,
      resourceRsp,
      namespace,
      resourceMetrics,
      edges,
      unmeshedSources,
      resourceIsMeshed,
      lastMetricReceivedTime,
      isTcpOnly,
      isTrafficSplit,
      loaded,
      error,
      upstreamMetrics,
      downstreamMetrics,
      podMetrics,
    } = this.state;
    const { pathPrefix } = this.props;

    if (!loaded && !error) {
      return <Spinner />;
    }

    const query = {
      resourceName,
      resourceType,
      namespace,
    };

    const unmeshed = _filter(unmeshedSources, d => d.type !== 'pod')
      .map(d => _merge({}, emptyMetric, d, {
        unmeshed: true,
        pods: {
          totalPods: d.pods.length,
          meshedPods: 0,
        },
      }));

    const upstreamDisplayMetrics = this.getDisplayMetrics(upstreamMetrics);
    const downstreamDisplayMetrics = this.getDisplayMetrics(downstreamMetrics);

    const upstreams = upstreamDisplayMetrics.concat(unmeshed);

    const showNoTrafficMsg = resourceIsMeshed && (Date.now() - lastMetricReceivedTime > showNoTrafficMsgDelayMs);

    if (isTrafficSplit) {
      return (
        <TrafficSplitDetail
          resourceType={resourceType}
          resourceName={resourceName}
          resourceMetrics={resourceMetrics}
          resourceRsp={resourceRsp} />
      );
    }
    return (
      <div>
        <Grid container justify="space-between" alignItems="center">
          <Grid item><Typography variant="h5">{resourceType}/{resourceName}</Typography></Grid>
          <Grid item>
            <Grid container spacing={1}>
              {showNoTrafficMsg ? <Grid item><SimpleChip label={<Trans>columnTitleNoTraffic</Trans>} type="warning" /></Grid> : null}
              <Grid item>
                {resourceIsMeshed ?
                  <SimpleChip label={<Trans>columnTitleMeshed</Trans>} type="good" /> :
                  <SimpleChip label={<Trans>columnTitleUnmeshed</Trans>} type="bad" />
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
          neighbors={{ upstream: upstreamDisplayMetrics, downstream: downstreamDisplayMetrics }}
          unmeshedSources={Object.values(unmeshedSources)}
          api={this.api} />

        {isTcpOnly ? null : <TopRoutesTabs
          query={query}
          pathPrefix={pathPrefix}
          updateUnmeshedSources={this.updateUnmeshedSources}
          disableTop={!resourceIsMeshed} />
        }

        {_isEmpty(upstreams) ? null :
        <MetricsTable
          resource="multi_resource"
          title={<Trans>tableTitleInbound</Trans>}
          metrics={upstreamDisplayMetrics} />
        }

        {_isEmpty(downstreamDisplayMetrics) ? null :
        <MetricsTable
          resource="multi_resource"
          title={<Trans>tableTitleOutbound</Trans>}
          metrics={downstreamDisplayMetrics} />
        }

        {
          resourceType === 'pod' || isTcpOnly ? null :
          <MetricsTable
            resource="pod"
            title={<Trans>tableTitlePods</Trans>}
            metrics={podMetrics} />
        }

        <MetricsTable
          resource="pod"
          title={<Trans>tableTitleTCP</Trans>}
          isTcpTable
          metrics={podMetrics} />

        <EdgesTable
          api={this.api}
          namespace={namespace}
          type={resourceType}
          edges={edges} />

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

ResourceDetailBase.propTypes = {
  api: PropTypes.shape({
    PrefixedLink: PropTypes.func.isRequired,
  }).isRequired,
  isPageVisible: PropTypes.bool.isRequired,
  match: PropTypes.shape({
    url: PropTypes.string.isRequired,
  }).isRequired,
  pathPrefix: PropTypes.string.isRequired,
};

export default withPageVisibility(withContext(ResourceDetailBase));
