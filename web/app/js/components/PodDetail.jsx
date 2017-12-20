import _ from 'lodash';
import { ApiHelpers } from './util/ApiHelpers.js';
import ConduitSpinner from "./ConduitSpinner.jsx";
import ErrorBanner from './ErrorBanner.jsx';
import HealthPane from './HealthPane.jsx';
import React from 'react';
import StatPane from './StatPane.jsx';
import UpstreamDownstream from './UpstreamDownstream.jsx';
import { processRollupMetrics, processTimeseriesMetrics } from './util/MetricUtils.js';
import 'whatwg-fetch';

export default class PodDetail extends React.Component {
  constructor(props) {
    super(props);
    this.api = ApiHelpers(this.props.pathPrefix);
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
      metricsWindow: "10m",
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

    let metricsUrl = `${this.props.pathPrefix}/api/metrics?window=${this.state.metricsWindow}` ;
    let podMetricsUrl = `${metricsUrl}&timeseries=true&target_pod=${this.state.pod}`;
    let upstreamRollupUrl = `${metricsUrl}&aggregation=source_pod&target_pod=${this.state.pod}`;
    let downstreamRollupUrl = `${metricsUrl}&aggregation=target_pod&source_pod=${this.state.pod}`;

    let podFetch = this.api.fetch(podMetricsUrl);
    let upstreamFetch =  this.api.fetch(upstreamRollupUrl);
    let downstreamFetch =  this.api.fetch(downstreamRollupUrl);

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
      <HealthPane
        key="pod-health-pane"
        entity={this.state.pod}
        entityType="pod"
        currentSr={currentSuccessRate}
        upstreamMetrics={this.state.upstreamMetrics}
        downstreamMetrics={this.state.downstreamMetrics} />,
      _.isEmpty(this.state.podTs) ? null :
        <StatPane
          key="pod-stat-pane"
          lastUpdated={this.state.lastUpdated}
          timeseries={this.state.podTs} />,
      <UpstreamDownstream
        key="pod-upstream-downstream"
        resource="pod"
        entity={this.state.pod}
        lastUpdated={this.state.lastUpdated}
        upstreamMetrics={this.state.upstreamMetrics}
        downstreamMetrics={this.state.downstreamMetrics}
        metricsWindow={this.state.metricsWindow}
        pathPrefix={this.props.pathPrefix} />
    ];
  }

  render() {
    return (
      <div className="page-content pod-detail">
        { !this.state.error ? null : <ErrorBanner message={this.state.error} /> }
        { !this.state.loaded ? <ConduitSpinner /> :
          <div>
            <div className="page-header">
              <div className="subsection-header">Pod detail</div>
              <h1>{this.state.pod}</h1>
            </div>
            {this.renderSections()}
          </div>
        }
      </div>
    );
  }
}
