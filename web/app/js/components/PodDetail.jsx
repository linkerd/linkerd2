import _ from 'lodash';
import ConduitSpinner from "./ConduitSpinner.jsx";
import HealthPane from './HealthPane.jsx';
import React from 'react';
import StatPane from './StatPane.jsx';
import UpstreamDownstream from './UpstreamDownstream.jsx';
import { processRollupMetrics, processTimeseriesMetrics } from './util/MetricUtils.js';
import 'whatwg-fetch';

export default class PodDetail extends React.Component {
  constructor(props) {
    super(props);
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
      upstreamTsByPod: {},
      downstreamMetrics: [],
      downstreamTsByPod: {},
      podTs: {},
      pendingRequests: false,
      loaded: false
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
    let upstreamTimeseriesUrl = `${upstreamRollupUrl}&timeseries=true`;
    let downstreamRollupUrl = `${metricsUrl}&aggregation=target_pod&source_pod=${this.state.pod}`;
    let downstreamTimeseriesUrl = `${downstreamRollupUrl}&timeseries=true`;

    let podFetch = fetch(podMetricsUrl).then(r => r.json());
    let upstreamFetch = fetch(upstreamRollupUrl).then(r => r.json());
    let upstreamTsFetch = fetch(upstreamTimeseriesUrl).then(r => r.json());
    let downstreamFetch = fetch(downstreamRollupUrl).then(r => r.json());
    let downstreamTsFetch = fetch(downstreamTimeseriesUrl).then(r => r.json());

    Promise.all([podFetch, upstreamFetch, upstreamTsFetch, downstreamFetch, downstreamTsFetch])
      .then(([podMetrics, upstreamRollup, upstreamTimeseries, downstreamRollup, downstreamTimeseries]) => {
        let podTs = processTimeseriesMetrics(podMetrics.metrics, "targetPod");
        let podTimeseries = _.get(podTs, this.state.pod, {});

        let upstreamMetrics = processRollupMetrics(upstreamRollup.metrics, "sourcePod");
        let upstreamTsByPod = processTimeseriesMetrics(upstreamTimeseries.metrics, "sourcePod");
        let downstreamMetrics = processRollupMetrics(downstreamRollup.metrics, "targetPod");
        let downstreamTsByPod = processTimeseriesMetrics(downstreamTimeseries.metrics, "targetPod");

        this.setState({
          pendingRequests: false,
          lastUpdated: Date.now(),
          podTs: podTimeseries,
          upstreamMetrics: upstreamMetrics,
          upstreamTsByPod: upstreamTsByPod,
          downstreamMetrics: downstreamMetrics,
          downstreamTsByPod: downstreamTsByPod,
          loaded: true
        });
      }).catch(() => {
        this.setState({ pendingRequests: false });
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
      <StatPane
        key="pod-stat-pane"
        lastUpdated={this.state.lastUpdated}
        timeseries={this.state.podTs} />,
      <UpstreamDownstream
        key="pod-upstream-downstream"
        entity="pod"
        lastUpdated={this.state.lastUpdated}
        upstreamMetrics={this.state.upstreamMetrics}
        upstreamTsByEntity={this.state.upstreamTsByPod}
        downstreamMetrics={this.state.downstreamMetrics}
        downstreamTsByEntity={this.state.downstreamTsByPod}
        pathPrefix={this.props.pathPrefix} />
    ];
  }

  renderContent() {
    if (_.isEmpty(this.state.podTs)) {
      return <div>No data</div>;
    } else {
      return this.renderSections();
    }
  }

  render() {
    if(!this.state.loaded){
      return <ConduitSpinner />;
    } else return (
      <div className="page-content pod-detail">
        <div className="page-header">
          <div className="subsection-header">Pod detail</div>
          <h1>{this.state.pod}</h1>
        </div>
        {this.renderContent()}
      </div>
    );
  }
}
