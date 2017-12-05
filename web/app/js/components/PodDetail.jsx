import React from 'react';
import _ from 'lodash';
import HealthPane from './HealthPane.jsx';
import StatPane from './StatPane.jsx';
import UpstreamDownstream from './UpstreamDownstream.jsx';
import { processMetrics, processTsWithLatencyBreakdown } from './util/MetricUtils.js';
import ConduitSpinner from "./ConduitSpinner.jsx";
export default class PodDetail extends React.Component {
  constructor(props) {
    super(props);
    this.loadFromServer = this.loadFromServer.bind(this);
    this.state = this.initialState(this.props.location);
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
      summaryMetrics: {},
      pendingRequests: false,
      loaded: false
    };
  }

  componentDidMount() {
    this.loadFromServer();
    this.timerId = window.setInterval(this.loadFromServer, this.state.pollingInterval);
  }

  componentWillUnmount() {
    window.clearInterval(this.timerId);
  }

  componentWillReceiveProps(nextProps) {
    window.scrollTo(0, 0);
    this.setState(this.initialState(nextProps.location), () => {
      this.loadFromServer();
    });
  }

  loadFromServer() {
    if (this.state.pendingRequests) {
      return; // don't make more requests if the ones we sent haven't completed
    }
    this.setState({ pendingRequests: true });

    let metricsUrl = `${this.props.pathPrefix}/api/metrics?window=${this.state.metricsWindow}` ;
    let podMetricsUrl = `${metricsUrl}&timeseries=true&target_pod=${this.state.pod}`;

    let upstreamRollupUrl = `${metricsUrl}&aggregation=source_pod&target_pod=${this.state.pod}`
    let upstreamTimeseriesUrl = `${upstreamRollupUrl}&timeseries=true`;
    let downstreamRollupUrl = `${metricsUrl}&aggregation=target_pod&source_pod=${this.state.pod}`
    let downstreamTimeseriesUrl = `${downstreamRollupUrl}&timeseries=true`;

    let podFetch = fetch(podMetricsUrl).then(r => r.json());
    let upstreamFetch = fetch(upstreamRollupUrl).then(r => r.json());
    let upstreamTsFetch = fetch(upstreamTimeseriesUrl).then(r => r.json());
    let downstreamFetch = fetch(downstreamRollupUrl).then(r => r.json());
    let downstreamTsFetch = fetch(downstreamTimeseriesUrl).then(r => r.json());

    Promise.all([podFetch, upstreamFetch, upstreamTsFetch, downstreamFetch, downstreamTsFetch])
      .then(([podMetrics, upstreamRollup, upsreamTimeseries, downstreamRollup, downstreamTimeseries]) => {
      let podTimeseries = processTsWithLatencyBreakdown(podMetrics.metrics);

      let upstreamMetrics = _.compact(processMetrics(upstreamRollup.metrics, upsreamTimeseries.metrics, "sourcePod"));
      let downstreamMetrics = _.compact(processMetrics(downstreamRollup.metrics, downstreamTimeseries.metrics, "targetPod"));

      this.setState({
        pendingRequests: false,
        lastUpdated: Date.now(),
        summaryMetrics: podTimeseries,
        upstreamMetrics: upstreamMetrics,
        downstreamMetrics: downstreamMetrics,
        loaded: true
      });
    });
  }

  renderSections() {
    return [
      <HealthPane
        key="pod-health-pane"
        entity={this.state.pod}
        entityType="pod"
        upstreamMetrics={this.state.upstreamMetrics}
        downstreamMetrics={this.state.downstreamMetrics}
      />,
      <StatPane
        key="pod-stat-pane"
        lastUpdated={this.state.lastUpdated}
        summaryMetrics={this.state.summaryMetrics}
      />,
      <UpstreamDownstream
        key="pod-upstream-downstream"
        entity="pod"
        lastUpdated={this.state.lastUpdated}
        upstreamMetrics={this.state.upstreamMetrics}
        downstreamMetrics={this.state.downstreamMetrics}
        pathPrefix={this.props.pathPrefix}
      />
    ];
  }

  renderContent() {
    if (_.isEmpty(this.state.summaryMetrics)) {
      return <div>No data</div>;
    } else {
      return this.renderSections();
    }
  }

  render() {
    if(!this.state.loaded){
      return <ConduitSpinner />
    } else return (
      <div className="page-content pod-detail">
        <div className="page-header">
          <div className="subsection-header">Pod detail</div>
          <h1>{this.state.pod}</h1>
          {this.renderContent()}
        </div>
      </div>
    );
  }
}
