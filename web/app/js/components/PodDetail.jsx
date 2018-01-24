import _ from 'lodash';
import ConduitSpinner from "./ConduitSpinner.jsx";
import ErrorBanner from './ErrorBanner.jsx';
import PageHeader from './PageHeader.jsx';
import React from 'react';
import ResourceHealthOverview from './ResourceHealthOverview.jsx';
import ResourceMetricsOverview from './ResourceMetricsOverview.jsx';
import UpstreamDownstream from './UpstreamDownstream.jsx';
import { processRollupMetrics, processTimeseriesMetrics } from './util/MetricUtils.js';
import 'whatwg-fetch';

export default class PodDetail extends React.Component {
  constructor(props) {
    super(props);
    this.api = this.props.api;
    this.handleApiError = this.handleApiError.bind(this);
    this.loadFromServer = this.loadFromServer.bind(this);
    this.state = this.initialState(this.props.location);
  }

  componentDidMount() {
    this.loadFromServer();
    this.timerId = window.setInterval(this.loadFromServer, this.state.pollingInterval);
  }

  componentWillReceiveProps(nextProps) {
    window.scrollTo(0, 0);
    this.setState(this.initialState(nextProps.location), () => {
      this.loadFromServer();
    });
  }

  componentWillUnmount() {
    window.clearInterval(this.timerId);
  }

  initialState(location) {
    let urlParams = new URLSearchParams(location.search);
    let pod = urlParams.get("pod");
    return {
      lastUpdated: 0,
      pollingInterval: 10000,
      pod: pod,
      upstreamMetrics: [],
      downstreamMetrics: [],
      podTs: {},
      pendingRequests: false,
      loaded: false,
      error: ''
    };
  }

  loadFromServer() {
    if (this.state.pendingRequests) {
      return; // don't make more requests if the ones we sent haven't completed
    }
    this.setState({ pendingRequests: true });

    let urls = this.api.urlsForResource;

    let metricsUrl = urls["deployment"].url().rollup;
    let podMetricsUrl = `${metricsUrl}&timeseries=true&target_pod=${this.state.pod}`;
    let upstreamRollupUrl = urls["upstream_pod"].url(this.state.pod).rollup;
    let downstreamRollupUrl = urls["downstream_pod"].url(this.state.pod).rollup;

    let podFetch = this.api.fetchMetrics(podMetricsUrl);
    let upstreamFetch =  this.api.fetchMetrics(upstreamRollupUrl);
    let downstreamFetch =  this.api.fetchMetrics(downstreamRollupUrl);

    Promise.all([podFetch, upstreamFetch, downstreamFetch])
      .then(([podMetrics, upstreamRollup, downstreamRollup]) => {
        let podTs = processTimeseriesMetrics(podMetrics.metrics, "targetPod");
        let podTimeseries = _.get(podTs, this.state.pod, {});

        let upstreamMetrics = processRollupMetrics(upstreamRollup.metrics, "sourcePod");
        let downstreamMetrics = processRollupMetrics(downstreamRollup.metrics, "targetPod");

        this.setState({
          pendingRequests: false,
          lastUpdated: Date.now(),
          podTs: podTimeseries,
          upstreamMetrics: upstreamMetrics,
          downstreamMetrics: downstreamMetrics,
          loaded: true,
          error: ''
        });
      }).catch(this.handleApiError);
  }

  handleApiError(e) {
    this.setState({
      pendingRequests: false,
      error: `Error getting data from server: ${e.message}`
    });
  }

  renderSections() {
    let currentSuccessRate = _.get(_.last(_.get(this.state.podTs, "SUCCESS_RATE", [])), "value");

    return [
      <ResourceHealthOverview
        key="pod-health-pane"
        resourceName={this.state.pod}
        resourceType="pod"
        currentSr={currentSuccessRate}
        upstreamMetrics={this.state.upstreamMetrics}
        downstreamMetrics={this.state.downstreamMetrics} />,
      _.isEmpty(this.state.podTs) ? null :
        <ResourceMetricsOverview
          key="pod-stat-pane"
          lastUpdated={this.state.lastUpdated}
          timeseries={this.state.podTs}
          window={this.api.getMetricsWindow()} />,
      <UpstreamDownstream
        key="pod-upstream-downstream"
        resourceType="pod"
        resourceName={this.state.pod}
        lastUpdated={this.state.lastUpdated}
        upstreamMetrics={this.state.upstreamMetrics}
        downstreamMetrics={this.state.downstreamMetrics}
        api={this.api} />
    ];
  }

  render() {
    return (
      <div className="page-content pod-detail">
        { !this.state.error ? null : <ErrorBanner message={this.state.error} /> }
        { !this.state.loaded ? <ConduitSpinner /> :
          <div>
            <PageHeader
              subHeaderTitle="Pod detail"
              subHeader={this.state.pod}
              api={this.api} />
            {this.renderSections()}
          </div>
        }
      </div>
    );
  }
}
